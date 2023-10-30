package tracker

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

type txTracingSampler = func(key txTracingId) bool
type txTracingId = string
type blockTracingSampler = func(blockNumber uint64) bool
type batchTracingSampler = func() bool

type samplerProvider interface {
	TxSampler() txTracingSampler
	BatchSampler() batchTracingSampler
	BlockSampler() blockTracingSampler
}

func NewReceiverSender(sampler samplerProvider, validLatency prometheus.Histogram, invalidLatency prometheus.Histogram) ReceiverSender {
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
