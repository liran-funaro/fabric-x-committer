package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
)

type perfMetrics struct {
	provider                        metrics.Provider
	blockSentTotal                  prometheus.Counter
	transactionSentTotal            prometheus.Counter
	transactionReceivedTotal        prometheus.Counter
	validTransactionLatencySecond   prometheus.Histogram
	invalidTransactionLatencySecond prometheus.Histogram
}

func newBlockgenServiceMetrics(p metrics.Provider) *perfMetrics {
	buckets := p.Buckets()
	return &perfMetrics{
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

type clientTracker struct {
	latencyTracker tracker.ReceiverSender
	metrics        *perfMetrics
}

func NewClientTracker(metrics *perfMetrics) tracker.ReceiverSender {
	return &clientTracker{
		metrics:        metrics,
		latencyTracker: tracker.NewReceiverSender(metrics.provider, metrics.validTransactionLatencySecond, metrics.invalidTransactionLatencySecond),
	}
}

func (c *clientTracker) OnReceiveTransaction(txID string, success bool) {
	c.metrics.transactionReceivedTotal.Inc()
	c.latencyTracker.OnReceiveTransaction(txID, success)
}

func (c *clientTracker) OnSendBlock(block *protoblocktx.Block) {
	c.metrics.blockSentTotal.Add(float64(1))
	c.metrics.transactionSentTotal.Add(float64(len(block.Txs)))
	c.latencyTracker.OnSendBlock(block)
}

func (c *clientTracker) OnSendTransaction(txId string) {
	c.metrics.transactionSentTotal.Add(float64(1))
	c.latencyTracker.OnSendTransaction(txId)
}
