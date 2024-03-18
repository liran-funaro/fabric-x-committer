package client

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	token "github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	coordinatorservice "github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload"
	"go.uber.org/ratelimit"
)

var logger = logging.New("clientadapter")

type CoordinatorAdapter struct {
	wg               sync.WaitGroup
	client           coordinatorservice.CoordinatorClient
	ctx              context.Context
	ctxCancel        context.CancelFunc
	receivedStatuses uint64
	rateLimiter      ratelimit.Limiter
}

func OpenCoordinatorAdapter(endpoint connection.Endpoint, rateLimiterConfig *loadgen.LimiterConfig) *CoordinatorAdapter {
	clientConfig := connection.NewDialConfig(endpoint)

	logger.Infof("Connect to coordinator on %v.\n", &endpoint)
	conn, err := connection.Connect(clientConfig)
	utils.Must(err)

	ctx, streamCancel := context.WithCancel(context.Background())
	client := coordinatorservice.NewCoordinatorClient(conn)

	return &CoordinatorAdapter{
		client:      client,
		ctx:         ctx,
		rateLimiter: loadgen.NewLimiter(rateLimiterConfig),
		ctxCancel:   streamCancel,
	}
}

func (c *CoordinatorAdapter) SetVerificationKey(publicKey signature.PublicKey) error {
	logger.Infof("Setting verification key.\n")
	key := &sigverification.Key{SerializedBytes: publicKey}
	_, err := c.client.SetMetaNamespaceVerificationKey(c.ctx, key)
	return err
}

func (c *CoordinatorAdapter) RunCommitterSubmitterListener(blocks <-chan *workload.BlockWithExpectedResult, onSubmit func(time.Time, *token.Block), onReceive func(*coordinatorservice.TxValidationStatusBatch)) {
	logger.Infof("Open stream to coordinator.\n")
	blockStream, err := c.client.BlockProcessing(c.ctx)
	utils.Must(err)

	// start receiver
	c.startCommitterOutputListener(blockStream, onReceive)

	// start consuming blocks

	c.runCommitterSubmitter(blockStream, blocks, onSubmit)
}

func (c *CoordinatorAdapter) startCommitterOutputListener(stream coordinatorservice.Coordinator_BlockProcessingClient, onReceive func(batch *coordinatorservice.TxValidationStatusBatch)) {
	logger.Infof("Starting response listener.\n")
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		logger.Infof("Spawning response listener...\n")
		for {
			response, err := stream.Recv()
			if err == io.EOF {
				// end of blockStream
				logger.Infof("RECV EOF\n")
				break
			}
			if err != nil {
				logger.Errorf("Closing listerer due to err: %v\n", err)
				break
			}

			logger.Infof("Received batch from committer with %d TXs.\n", len(response.GetTxsValidationStatus()))
			atomic.AddUint64(&c.receivedStatuses, uint64(len(response.GetTxsValidationStatus())))

			onReceive(response)
		}
	}()
}

func (c *CoordinatorAdapter) runCommitterSubmitter(stream coordinatorservice.Coordinator_BlockProcessingClient, dQueue <-chan *workload.BlockWithExpectedResult, onSend func(time.Time, *token.Block)) {
	logger.Infof("Start submitter to coordinator.\n")
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
	logger.Infof("\nstopped sending! sent: %d received: %d\n", txsSent, c.receivedStatuses)
	logger.Infof("waiting for %d to complete\n", needToComplete)

	if needToComplete > 0 {
		// receiver interrupt
		interrupt(c.ctxCancel)
		c.wg.Wait()
	}

	totalElapsed := time.Since(start)
	workload.PrintStats(txsSent, blocksSent, int64(atomic.LoadUint64(&c.receivedStatuses)), elapsedPushed, totalElapsed)
}

func (c *CoordinatorAdapter) send(ctx context.Context, stream coordinatorservice.Coordinator_BlockProcessingClient, bQueue <-chan *workload.BlockWithExpectedResult, cnt func(t time.Time, b *token.Block)) {
	for {
		select {
		case <-ctx.Done():
			return
		case b, more := <-bQueue:
			c.rateLimiter.Take()
			if !more {
				// no more blocks to send
				return
			}

			t := time.Now()
			if err := stream.SendMsg(b.Block); err != nil {
				if err == io.EOF {
					// end of blockStream
					logger.Infof("RECV EOF\n")
					return
				}
				utils.Must(err)
				logger.Infof("Sent block %d:%d to coordinator.\n", b.Block.Number, len(b.Block.Txs))
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
