package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("tracker")

type TransactionReceiver interface {
	RegisterLatency(duration time.Duration, success bool)
}

type Receiver interface {
	OnReceiveTransaction(txID string, success bool)
}

type Sender interface {
	OnSendBlock(block *protoblocktx.Block)
	OnSendTransaction(txId string)
}

type ReceiverSender interface {
	Receiver
	Sender
}

type latencyReceiverSender struct {
	Receiver
	Sender
}

type (
	txTracingSampler    = func(key txTracingID) bool
	txTracingID         = string
	blockTracingSampler = func(blockNumber uint64) bool
	batchTracingSampler = func() bool
)

type samplerProvider interface {
	TxSampler() txTracingSampler
	BatchSampler() batchTracingSampler
	BlockSampler() blockTracingSampler
}

// NewReceiverSender instantiate latencyReceiverSender.
func NewReceiverSender(sampler samplerProvider, validLatency, invalidLatency prometheus.Histogram) ReceiverSender {
	latencyTracker := &sync.Map{}
	return &latencyReceiverSender{
		Receiver: &latencyReceiverTracker{
			latencyTracker: latencyTracker,
			validLatency:   validLatency,
			invalidLatency: invalidLatency,
		},
		Sender: &latencySenderTracker{
			latencyTracker: latencyTracker,
			batchSampler:   sampler.BatchSampler(),
			blockSampler:   sampler.BlockSampler(),
			txSampler:      sampler.TxSampler(),
		},
	}
}
