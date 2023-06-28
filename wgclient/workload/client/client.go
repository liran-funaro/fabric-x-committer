package client

import (
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload"
	_ "github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload/client/codec"
	"google.golang.org/protobuf/proto"
)

func GetBlockSize(path string, sampleSize int) float64 {
	pp := workload.LoadProfileFromYaml(path)
	return workload.GetBlockSize(pp, sampleSize)
}

func LoadAndPump(path, endpoint, prometheusEndpoint, latencyEndpoint string) {
	// read blocks from file into channel
	tracker := newMetricTracker(monitoring.Config{
		Latency: &latency.Config{
			Endpoint: connection.CreateEndpoint(latencyEndpoint),
		},
		Metrics: &metrics.Config{
			Endpoint: connection.CreateEndpoint(prometheusEndpoint),
		},
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

func GenerateAndPump(config workload.BlockgenStreamConfig) {
	pp := workload.LoadProfileFromYaml(config.Generator.Profile)

	tracker := newMetricTracker(config.Monitoring)
	// generate blocks and push them into channel
	publicKey, bQueue := workload.StartBlockGenerator(pp)

	// event collector
	eventQueue := make(chan *workload.Event, 10000)

	go func() {
		for e := range eventQueue {
			tracker.RegisterEvent(e)
		}
	}()

	PumpToCoordinator(publicKey, bQueue, eventQueue, pp, config.Endpoint.Address())
}

func Validate(path string) {

	workload.Validate(path)
}

func PumpToCoordinator(serializedKey []byte, dQueue <-chan *workload.BlockWithExpectedResult, eventQueue chan *workload.Event, pp *workload.Profile, endpoint string) {
	cl := OpenCoordinatorAdapter(*connection.CreateEndpoint(endpoint))

	utils.Must(cl.SetVerificationKey(serializedKey))

	onReceive := func(response *coordinatorservice.TxValidationStatusBatch) {
		if eventQueue != nil {
			eventQueue <- &workload.Event{
				Timestamp:   time.Now(),
				Msg:         workload.EventReceived,
				StatusBatch: response,
			}
		}
	}

	// TODO double check if we can simplify our life by using the request tracker in utils/connection/request_tracker.go
	bar := workload.NewProgressBar("Sending blocks from file...", pp.Block.Count, "blocks")

	onSubmit := func(t time.Time, block *token.Block) {
		bar.Add(1)
		if eventQueue != nil {
			eventQueue <- &workload.Event{
				Timestamp:      t,
				Msg:            workload.EventSubmitted,
				SubmittedBlock: &workload.BlockInfo{Id: block.Number, Size: len(block.Txs)},
			}
		}
	}

	cl.RunCommitterSubmitterListener(dQueue, onSubmit, onReceive)

	if eventQueue != nil {
		close(eventQueue)
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
	bar := workload.NewProgressBar("Reading blocks from file...", pp.Block.Count, "blocks")
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

type metricTracker struct {
	*workload.MetricTracker
}

func newMetricTracker(p monitoring.Config) *metricTracker {
	return &metricTracker{workload.NewMetricTracker(p)}
}
func (t *metricTracker) RegisterEvent(e *workload.Event) {
	switch e.Msg {
	case workload.EventSubmitted:
		for i := uint64(0); i < uint64(e.SubmittedBlock.Size); i++ {
			t.TxSentAt(token.TxSeqNum{e.SubmittedBlock.Id, i}, e.Timestamp)
		}
	case workload.EventReceived:
		for _, status := range e.StatusBatch.TxsValidationStatus {
			t.TxReceivedAt(token.TxSeqNum{status.BlockNum, status.TxNum}, status.Status, e.Timestamp)
		}
	}
}
