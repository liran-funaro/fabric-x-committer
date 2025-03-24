package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	promgo "github.com/prometheus/client_model/go"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

// PerfMetrics is a struct that contains the metrics for the block generator.
type PerfMetrics struct {
	*monitoring.Provider

	blockSentTotal                  *monitoring.IntCounter
	transactionSentTotal            *monitoring.IntCounter
	transactionReceivedTotal        *monitoring.IntCounter
	transactionCommittedTotal       *monitoring.IntCounter
	transactionAbortedTotal         *monitoring.IntCounter
	validTransactionLatencySecond   prometheus.Histogram
	invalidTransactionLatencySecond prometheus.Histogram

	latencyTracker ReceiverSender
}

// NewLoadgenServiceMetrics creates a new PerfMetrics instance.
func NewLoadgenServiceMetrics(c *Config) *PerfMetrics {
	p := monitoring.NewProvider()
	buckets := c.Latency.BucketConfig.Buckets()
	m := &PerfMetrics{
		Provider: p,
		blockSentTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "loadgen",
			Name:      "block_sent_total",
			Help:      "Total number of blocks sent by the block generator",
		}),
		transactionSentTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "loadgen",
			Name:      "transaction_sent_total",
			Help:      "Total number of transactions sent by the block generator",
		}),
		transactionReceivedTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "loadgen",
			Name:      "transaction_received_total",
			Help:      "Total number of transactions received by the block generator",
		}),
		transactionCommittedTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "loadgen",
			Name:      "transaction_committed_total",
			Help:      "Total number of transaction commit statuses received by the block generator",
		}),
		transactionAbortedTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "loadgen",
			Name:      "transaction_aborted_total",
			Help:      "Total number of transaction abort statuses received by the block generator",
		}),
		validTransactionLatencySecond: p.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loadgen",
			Subsystem: "",
			Name:      "valid_transaction_latency_seconds",
			Help:      "Latency of transactions in seconds",
			Buckets:   buckets,
		}),
		invalidTransactionLatencySecond: p.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loadgen",
			Subsystem: "",
			Name:      "invalid_transaction_latency_seconds",
			Help:      "Latency of invalid transactions in seconds",
			Buckets:   buckets,
		}),
	}
	m.latencyTracker = NewReceiverSender(
		&c.Latency.SamplerConfig, m.validTransactionLatencySecond, m.invalidTransactionLatencySecond,
	)
	return m
}

// OnReceiveTransaction is a function that increments the transaction received total
// and calls the latency tracker.
func (c *PerfMetrics) OnReceiveTransaction(txID string, status protoblocktx.Status) {
	c.transactionReceivedTotal.Inc()
	success := status == protoblocktx.Status_COMMITTED
	if success {
		c.transactionCommittedTotal.Inc()
	} else {
		c.transactionAbortedTotal.Inc()
	}
	c.latencyTracker.OnReceiveTransaction(txID, success)
}

// GetCommitted returns the number of committed transactions.
func (c *PerfMetrics) GetCommitted() (uint64, error) {
	gm := promgo.Metric{}
	err := c.transactionCommittedTotal.Write(&gm)
	if err != nil {
		return 0, err
	}
	return uint64(gm.Counter.GetValue()), nil
}

// OnSendBlock is a function that increments the block sent total and calls the latency tracker.
func (c *PerfMetrics) OnSendBlock(block *protocoordinatorservice.Block) {
	c.blockSentTotal.Add(1)
	c.transactionSentTotal.Add(len(block.Txs))
	c.latencyTracker.OnSendBlock(block)
}

// OnSendTransaction is a function that increments the transaction sent total and calls the latency tracker.
func (c *PerfMetrics) OnSendTransaction(txID string) {
	c.transactionSentTotal.Add(1)
	c.latencyTracker.OnSendTransaction(txID)
}
