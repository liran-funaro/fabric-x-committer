package workload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gocarina/gocsv"
	"github.ibm.com/distributed-trust-research/scalable-committer/protos/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

type BlockInfo struct {
	Id   uint64
	Size int
}

type Event struct {
	Timestamp      time.Time
	Msg            string
	SubmittedBlock *BlockInfo
	StatusBatch    *coordinatorservice.TxValidationStatusBatch
}

const (
	EventSubmitted = "submitted"
	EventReceived  = "received"
)

type Result struct {
	TxId           string // blockId-TxId
	SubmitTime     time.Time
	RecvTime       time.Time
	ExpectedStatus coordinatorservice.Status
	ActualStatus   coordinatorservice.Status
	Latency        time.Duration
}

type ExpectedResult struct {
	Reason coordinatorservice.Status
}

type EventStore struct {
	f      *os.File
	writer *bufio.Writer
}

func NewEventStore(path string) *EventStore {
	f, err := os.Create(path + ".events")
	utils.Must(err)

	return &EventStore{writer: bufio.NewWriter(f)}
}

func (es *EventStore) Store(e *Event) {
	b, err := json.Marshal(e)
	utils.Must(err)

	es.writer.Write(b)
	es.writer.WriteString("\n")
}

func (es *EventStore) Close() {
	es.writer.Flush()
	es.f.Close()
}

func Validate(path string) {

	// read blocks from file into channel
	// TODO remove this ugly suffix
	_, blockQueue, _ := GetBlockWorkload(strings.TrimSuffix(path, ".events"))
	eventQueue, _ := GetEvents(path)

	var results []*Result
	lookup := make(map[string]int)

	// iterate through the all blocks
	fmt.Printf("Parsing blocks ...\n")
	cnt := 0
	for b := range blockQueue {
		for _, r := range b.ExpectedResults {
			txid := fmt.Sprintf("%d-%d", r.BlockNum, r.TxNum)
			res := &Result{
				TxId:           txid,
				ExpectedStatus: r.Status,
			}

			results = append(results, res)
			lookup[txid] = cnt
			cnt++
		}
	}

	// parse all events!
	fmt.Printf("Parsing events ...\n")
	eventCount := 0
	for event := range eventQueue {

		if event.Msg == EventSubmitted {

			// let's walk through all tx in a block submitted event
			for i := 0; i < event.SubmittedBlock.Size; i++ {
				txid := fmt.Sprintf("%d-%d", event.SubmittedBlock.Id, i)
				idx, ok := lookup[txid]
				if !ok {
					panic("ahhh")
				}

				r := results[idx]

				r.SubmitTime = event.Timestamp
				if !r.SubmitTime.IsZero() && !r.RecvTime.IsZero() {
					r.Latency = r.RecvTime.Sub(r.SubmitTime)
				}
			}

		} else if event.Msg == EventReceived {

			for _, s := range event.StatusBatch.GetTxsValidationStatus() {
				txid := fmt.Sprintf("%d-%d", s.BlockNum, s.TxNum)
				idx, ok := lookup[txid]
				if !ok {
					panic("ahhh received not found " + txid)
				}
				r := results[idx]

				r.RecvTime = event.Timestamp
				r.ActualStatus = s.Status
				if !r.SubmitTime.IsZero() && !r.RecvTime.IsZero() {
					r.Latency = r.RecvTime.Sub(r.SubmitTime)
				}
			}
		}
		eventCount++
	}

	first := time.Now()
	last := time.Time{}

	// collecting latencies
	fmt.Printf("Collecting tx latencies ...\n")
	totalTxCount := 0
	issuesFound := 0
	for _, p := range results {
		if p.SubmitTime.Before(first) {
			first = p.SubmitTime
		}

		if p.RecvTime.After(last) {
			last = p.RecvTime
		}

		if p.SubmitTime.IsZero() || p.RecvTime.IsZero() || p.ExpectedStatus != p.ActualStatus {
			issuesFound++
			// only print issues
			fmt.Printf("  %s, %v, %v, %v, %v, %v\n", p.TxId, p.SubmitTime, p.RecvTime, p.ExpectedStatus, p.ActualStatus, p.Latency)
		}

		totalTxCount++
	}

	fmt.Printf("events processed: %d\n", eventCount)
	fmt.Printf("tx: %d\n", totalTxCount)
	fmt.Printf("issues found: %d\n", issuesFound)
	fmt.Printf("total experiment duration: %v\n", last.Sub(first))

	// write to csv
	// TODO

	f, err := os.Create(path + ".cvs")
	defer f.Close()
	utils.Must(err)

	err = gocsv.MarshalFile(&results, f)
	utils.Must(err)
}
