package main

import (
	"sync"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/aggregator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
)

var buckets = []float64{
	.0001, .001, .002, .003, .004, .005, .01, .03, .05,
	.1, .3, .5, 1, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 2.0,
	2.1, 2.2, 2.3, 2.4, 2.5, 3, 4, 5,
}

type perfMetrics struct {
	enabled                         bool
	provider                        *prometheusmetrics.Provider
	blockSentTotal                  prometheus.Counter
	transactionSentTotal            prometheus.Counter
	transactionReceivedTotal        prometheus.Counter
	validTransactionLatencySecond   prometheus.Histogram
	invalidTransactionLatencySecond prometheus.Histogram
}

func newBlockgenServiceMetrics(enabled bool) *perfMetrics {
	p := prometheusmetrics.NewProvider()

	return &perfMetrics{
		enabled:  enabled,
		provider: p,
		blockSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "block_sent_total",
			Help:      "Total number of blocks sent by the block generator",
		}),
		transactionSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_sent_total",
			Help:      "Total number of transactions sent by the block generator",
		}),
		transactionReceivedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_received_total",
			Help:      "Total number of transactions received by the block generator",
		}),
		validTransactionLatencySecond: p.NewHistogram(prometheus.HistogramOpts{
			Namespace: "blockgen",
			Subsystem: "",
			Name:      "valid_transaction_latency_seconds",
			Help:      "Latency of transactions in seconds",
			Buckets:   buckets,
		}),
		invalidTransactionLatencySecond: p.NewHistogram(prometheus.HistogramOpts{
			Namespace: "blockgen",
			Subsystem: "",
			Name:      "invalid_transaction_latency_seconds",
			Help:      "Latency of invalid transactions in seconds",
			Buckets:   buckets,
		}),
	}
}

func (s *perfMetrics) addToCounter(c prometheus.Counter, n int) {
	if s.enabled {
		c.Add(float64(n))
	}
}

type ClientTracker struct {
	*senderTracker
	*receiverTracker
}

func NewClientTracker(logger CmdLogger, metrics *perfMetrics, samplerConfig latency.SamplerConfig) *ClientTracker {
	latencyTracker := &sync.Map{}
	return &ClientTracker{
		senderTracker: &senderTracker{
			logger:         logger,
			metrics:        metrics,
			latencyTracker: latencyTracker,
			blockSampler:   samplerConfig.BlockSampler(),
			txSampler:      samplerConfig.TxSampler(),
		},
		receiverTracker: &receiverTracker{
			logger:         logger,
			metrics:        metrics,
			latencyTracker: latencyTracker,
		},
	}
}

type ReceiverTracker interface {
	OnReceiveTransaction(txID string, status protoblocktx.Status)
	OnReceiveBlock(block *common.Block)
	OnReceiveVCBatch(batch *protovcservice.TransactionStatus)
	OnReceiveCoordinatorBatch(batch *protocoordinatorservice.TxValidationStatusBatch)
}

type receiverTracker struct {
	logger         CmdLogger
	metrics        *perfMetrics
	latencyTracker *sync.Map
}

func (c *receiverTracker) OnReceiveCoordinatorBatch(batch *protocoordinatorservice.TxValidationStatusBatch) {
	for _, tx := range batch.TxsValidationStatus {
		c.OnReceiveTransaction(tx.TxId, tx.Status)
	}
}

func (c *receiverTracker) OnReceiveVCBatch(batch *protovcservice.TransactionStatus) {
	for id, status := range batch.Status {
		c.OnReceiveTransaction(id, status)
	}
}

func (c *receiverTracker) OnReceiveTransaction(txID string, status protoblocktx.Status) {
	c.metrics.addToCounter(c.metrics.transactionReceivedTotal, 1)
	if t, ok := c.latencyTracker.LoadAndDelete(txID); ok {
		start, _ := t.(time.Time)
		elapsed := time.Since(start)

		if status == protoblocktx.Status_COMMITTED {
			prometheusmetrics.Observe(c.metrics.validTransactionLatencySecond, elapsed)
		} else {
			prometheusmetrics.Observe(c.metrics.invalidTransactionLatencySecond, elapsed)
		}
	}
}

func (c *receiverTracker) OnReceiveBlock(block *common.Block) {
	statusCodes := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
	for i, data := range block.Data.Data {
		if _, channelHeader, err := serialization.UnwrapEnvelope(data); err == nil {
			c.OnReceiveTransaction(channelHeader.TxId, aggregator.StatusInverseMap[statusCodes[i]])
		}
	}
}

type SenderTracker interface {
	OnSendBlock(block *protoblocktx.Block)
}

type senderTracker struct {
	logger         CmdLogger
	metrics        *perfMetrics
	latencyTracker *sync.Map
	blockSampler   latency.BlockTracingSampler
	txSampler      latency.TxTracingSampler
}

func (c *senderTracker) OnSendBlock(block *protoblocktx.Block) {
	c.metrics.addToCounter(c.metrics.blockSentTotal, 1)
	c.metrics.addToCounter(c.metrics.transactionSentTotal, len(block.Txs))
	if c.blockSampler(block.Number) {
		t := time.Now()
		for _, tx := range block.Txs {
			c.latencyTracker.Store(tx.Id, t)
		}
	}
}

func (c *senderTracker) OnSendTransaction(txId string) {
	c.metrics.addToCounter(c.metrics.transactionSentTotal, 1)

	if c.txSampler(txId) {
		c.latencyTracker.Store(txId, time.Now())
	}
}
