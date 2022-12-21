package client

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

type CoordinatorAdapter struct {
	wg               sync.WaitGroup
	client           coordinatorservice.CoordinatorClient
	ctx              context.Context
	ctxCancel        context.CancelFunc
	receivedStatuses uint64
}

func OpenCoordinatorAdapter(endpoint connection.Endpoint) *CoordinatorAdapter {
	clientConfig := connection.NewDialConfig(endpoint)

	fmt.Printf("Connect to coordinator...\n")
	conn, err := connection.Connect(clientConfig)
	utils.Must(err)

	ctx, streamCancel := context.WithCancel(context.Background())
	client := coordinatorservice.NewCoordinatorClient(conn)

	return &CoordinatorAdapter{
		client:    client,
		ctx:       ctx,
		ctxCancel: streamCancel,
	}
}

func (c *CoordinatorAdapter) SetVerificationKey(publicKey signature.PublicKey) error {
	key := &sigverification.Key{SerializedBytes: publicKey}
	_, err := c.client.SetVerificationKey(c.ctx, key)
	return err
}

func (c *CoordinatorAdapter) RunCommitterSubmitterListener(blocks chan *workload.BlockWithExpectedResult, onSubmit func(time.Time, *token.Block), onReceive func(*coordinatorservice.TxValidationStatusBatch)) {
	blockStream, err := c.client.BlockProcessing(c.ctx)
	utils.Must(err)

	// start receiver
	c.startCommitterOutputListener(blockStream, onReceive)

	// start consuming blocks

	c.runCommitterSubmitter(blockStream, blocks, onSubmit)
}

func (c *CoordinatorAdapter) startCommitterOutputListener(stream coordinatorservice.Coordinator_BlockProcessingClient, onReceive func(batch *coordinatorservice.TxValidationStatusBatch)) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		fmt.Printf("Spawning response listener...\n")
		for {
			response, err := stream.Recv()
			if err == io.EOF {
				// end of blockStream
				fmt.Printf("RECV EOF\n")
				break
			}

			if err != nil {
				fmt.Printf("Closing listerer due to err: %v\n", err)
				break
			}

			atomic.AddUint64(&c.receivedStatuses, uint64(len(response.GetTxsValidationStatus())))

			onReceive(response)
		}
	}()
}

func (c *CoordinatorAdapter) runCommitterSubmitter(stream coordinatorservice.Coordinator_BlockProcessingClient, dQueue chan *workload.BlockWithExpectedResult, onSend func(time.Time, *token.Block)) {
	// sender context
	ctx, cancel := context.WithCancel(context.Background())
	// sender interrupt
	interrupt(cancel)

	start := time.Now()
	txsSent := int64(0)
	blocksSent := int64(0)
	c.send(ctx, stream, dQueue, func(t time.Time, block *token.Block) {
		txsSent += int64(len(block.GetTxs()))
		blocksSent += 1
		onSend(t, block)
	})
	elapsedPushed := time.Since(start)

	err := stream.CloseSend()
	utils.Must(err)

	needToComplete := txsSent - int64(atomic.LoadUint64(&c.receivedStatuses))
	fmt.Printf("\nstopped sending! sent: %d received: %d\n", txsSent, c.receivedStatuses)
	fmt.Printf("waiting for %d to complete\n", needToComplete)

	if needToComplete > 0 {
		// receiver interrupt
		interrupt(c.ctxCancel)
		c.wg.Wait()
	}

	totalElapsed := time.Since(start)
	workload.PrintStats(txsSent, blocksSent, int64(atomic.LoadUint64(&c.receivedStatuses)), elapsedPushed, totalElapsed)
}

func (c *CoordinatorAdapter) send(ctx context.Context, stream coordinatorservice.Coordinator_BlockProcessingClient, bQueue chan *workload.BlockWithExpectedResult, cnt func(t time.Time, b *token.Block)) {
	for {
		select {
		case <-ctx.Done():
			return
		case b, more := <-bQueue:
			if !more {
				// no more blocks to send
				return
			}

			t := time.Now()
			if err := stream.SendMsg(b.Block); err != nil {
				if err == io.EOF {
					// end of blockStream
					fmt.Printf("RECV EOF\n")
					return
				}
				utils.Must(err)
			}
			cnt(t, b.Block)
		}
	}
}

func interrupt(cancel context.CancelFunc) {
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		cancel()
	}()
}
