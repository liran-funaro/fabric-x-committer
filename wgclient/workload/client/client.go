package client

import (
	"context"
	"encoding/base64"
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
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client/codec"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

func GetBlockSize(path string, sampleSize int) float64 {
	pp := workload.LoadProfileFromYaml(path)
	return workload.GetBlockSize(pp, sampleSize)
}

func LoadAndPump(path, endpoint, prometheusEndpoint, latencyEndpoint string) {
	// read blocks from file into channel
	tracker := workload.NewMetricTracker(monitoring.Prometheus{
		LatencyEndpoint: *connection.CreateEndpoint(latencyEndpoint),
		Endpoint:        *connection.CreateEndpoint(prometheusEndpoint),
	})
	serializedKey, bQueue, pp := workload.GetBlockWorkload(path)

	// event collector
	eventQueue := make(chan *workload.Event, 10000)
	eventStore := workload.NewEventStore(path)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for e := range eventQueue {
			eventStore.Store(e)
			tracker.RegisterEvent(e)
		}
		eventStore.Close()
	}()

	PumpToCoordinator(serializedKey, bQueue, eventQueue, pp, endpoint)
	wg.Wait()
	<-time.After(workload.ScrapingInterval)
}

func GenerateAndPump(profilePath, endpoint, prometheusEndpoint, latencyEndpoint string) {
	pp := workload.LoadProfileFromYaml(profilePath)

	tracker := workload.NewMetricTracker(monitoring.Prometheus{
		Endpoint:        *connection.CreateEndpoint(prometheusEndpoint),
		LatencyEndpoint: *connection.CreateEndpoint(latencyEndpoint),
	})
	// generate blocks and push them into channel
	publicKey, bQueue := workload.StartBlockGenerator(pp)

	// event collector
	eventQueue := make(chan *workload.Event, 10000)

	go func() {
		for e := range eventQueue {
			tracker.RegisterEvent(e)
		}
	}()

	PumpToCoordinator(publicKey, bQueue, eventQueue, pp, endpoint)
}

func Validate(path string) {

	workload.Validate(path)
}

func PumpToCoordinator(serializedKey []byte, dQueue chan *workload.BlockWithExpectedResult, eventQueue chan *workload.Event, pp *workload.Profile, endpoint string) {

	clientConfig := connection.NewDialConfig(*connection.CreateEndpoint(endpoint))

	fmt.Printf("Connect to coordinator...\n")
	conn, err := connection.Connect(clientConfig)
	utils.Must(err)

	ctx, streamCancel := context.WithCancel(context.Background())
	client := coordinatorservice.NewCoordinatorClient(conn)

	// send key
	key := &sigverification.Key{SerializedBytes: serializedKey}
	_, err = client.SetVerificationKey(ctx, key)
	utils.Must(err)

	blockStream, err := client.BlockProcessing(ctx)
	utils.Must(err)

	// start receiver
	receivedStatuses := uint64(0)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Printf("Spawning response listener...\n")
		for {
			response, err := blockStream.Recv()
			if err == io.EOF {
				// end of blockStream
				fmt.Printf("RECV EOF\n")
				break
			}

			if err != nil {
				fmt.Printf("Closing listerer due to err: %v\n", err)
				break
			}
			_ = response

			atomic.AddUint64(&receivedStatuses, uint64(len(response.GetTxsValidationStatus())))

			// track response
			if eventQueue != nil {
				eventQueue <- &workload.Event{
					Timestamp:   time.Now(),
					Msg:         workload.EventReceived,
					StatusBatch: response,
				}
			}
		}
	}()

	// sender context
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// sender interrupt
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		cancel()
	}()

	// start consuming blocks
	// TODO double check if we can simplify our life by using the request tracker in utils/connection/request_tracker.go
	bar := workload.NewProgressBar("Sending blocks from file...", pp.Block.Count)
	start := time.Now()
	txsSent := int64(0)
	send(ctx, blockStream, dQueue, func(t time.Time, block *token.Block) {
		bar.Add(1)
		txsSent += int64(len(block.GetTxs()))

		// track submissions
		if eventQueue != nil {
			eventQueue <- &workload.Event{
				Timestamp:      t,
				Msg:            workload.EventSubmitted,
				SubmittedBlock: &workload.BlockInfo{Id: block.Number, Size: len(block.Txs)},
			}
		}
	})
	elapsedPushed := time.Since(start)

	err = blockStream.CloseSend()
	utils.Must(err)

	needToComplete := txsSent - int64(atomic.LoadUint64(&receivedStatuses))
	fmt.Printf("\nstopped sending! sent: %d received: %d\n", txsSent, receivedStatuses)
	fmt.Printf("waiting for %d to complete\n", needToComplete)

	if needToComplete > 0 {
		go func() {
			// receiver interrupt
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)
			<-c
			streamCancel()
		}()
		wg.Wait()
	}

	totalElapsed := time.Since(start)
	blocksSent := int64(bar.State().CurrentBytes)
	workload.PrintStats(txsSent, blocksSent, int64(atomic.LoadUint64(&receivedStatuses)), elapsedPushed, totalElapsed)

	if eventQueue != nil {
		close(eventQueue)
	}
}

func send(ctx context.Context, blockStream grpc.ClientStream, bQueue chan *workload.BlockWithExpectedResult, cnt func(t time.Time, b *token.Block)) {
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
			if err := blockStream.SendMsg(b.Block); err != nil {
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

func ReadAndForget(path string) {

	_, dQueue, pp := workload.GetByteWorkload(path)

	numTx := pp.Block.Count * pp.Block.Size

	// let's check for duplicates
	// be careful - this may cause damage on your computer if numTx is massive!  :D
	stats := make(map[string]string, numTx)
	allowedDuplicates := 0
	foundDuplicates := 0

	// start consuming blocks
	start := time.Now()
	bar := workload.NewProgressBar("Reading blocks from file...", pp.Block.Count)
	for b := range dQueue {
		_ = b
		block := &token.Block{}
		err := proto.Unmarshal(b, block)
		utils.Must(err)
		_ = block

		// check if we have duplicates
		for i, tx := range block.Txs {
			for j, sn := range tx.SerialNumbers {
				if len(sn) != 32 {
					panic("len wrong")
				}

				// let's treat the sn as base64 string
				k := base64.StdEncoding.EncodeToString(sn)
				if val, exists := stats[k]; exists {
					fmt.Printf("Duplicate found:\n")
					fmt.Printf("%s exists for: %s\n", k, val)
					fmt.Printf("tx: %d, %d, %d\n\n", block.Number, i, j)
					foundDuplicates++
					if foundDuplicates > allowedDuplicates {
						panic(fmt.Sprintf("too many duplicates!!! found %d, allowed: %d", foundDuplicates, allowedDuplicates))
					}
				}
				stats[k] = fmt.Sprintf("tx-%d-%d-%d", block.Number, i, j)
			}
		}

		bar.Add(1)
	}
	elapsed := time.Since(start)
	workload.PrintStats(pp.Block.Count*pp.Block.Size, pp.Block.Count, 0, elapsed, 0)
}
