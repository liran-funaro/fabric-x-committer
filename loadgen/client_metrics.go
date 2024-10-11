package loadgen

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
)

// PerfMetrics is a struct that contains the metrics for the block generator.
type PerfMetrics struct {
	provider                        metrics.Provider
	blockSentTotal                  *metrics.IntCounter
	transactionSentTotal            *metrics.IntCounter
	transactionReceivedTotal        *metrics.IntCounter
	transactionCommittedTotal       *metrics.IntCounter
	transactionAbortedTotal         *metrics.IntCounter
	validTransactionLatencySecond   prometheus.Histogram
	invalidTransactionLatencySecond prometheus.Histogram
}

func newBlockgenServiceMetrics(p metrics.Provider) *PerfMetrics {
	buckets := p.Buckets()
	return &PerfMetrics{
		provider: p,
		blockSentTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "block_sent_total",
			Help:      "Total number of blocks sent by the block generator",
		}),
		transactionSentTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_sent_total",
			Help:      "Total number of transactions sent by the block generator",
		}),
		transactionReceivedTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_received_total",
			Help:      "Total number of transactions received by the block generator",
		}),
		transactionCommittedTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_committed_total",
			Help:      "Total number of transaction commit statuses received by the block generator",
		}),
		transactionAbortedTotal: p.NewIntCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_aborted_total",
			Help:      "Total number of transaction abort statuses received by the block generator",
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

// ClientTracker is a struct that contains the latency tracker and the metrics for the client.
type ClientTracker struct {
	latencyTracker tracker.ReceiverSender
	metrics        *PerfMetrics
}

// NewClientTracker is a constructor for the clientTracker struct.
func NewClientTracker(m *PerfMetrics) *ClientTracker {
	return &ClientTracker{
		metrics: m,
		latencyTracker: tracker.NewReceiverSender(
			m.provider, m.validTransactionLatencySecond, m.invalidTransactionLatencySecond),
	}
}

// OnReceiveTransaction is a function that increments the transaction received total
// and calls the latency tracker.
func (c *ClientTracker) OnReceiveTransaction(txID string, success bool) {
	c.metrics.transactionReceivedTotal.Inc()
	if success {
		c.metrics.transactionCommittedTotal.Inc()
	} else {
		c.metrics.transactionAbortedTotal.Inc()
	}
	c.latencyTracker.OnReceiveTransaction(txID, success)
}

// OnSendBlock is a function that increments the block sent total and calls the latency tracker.
func (c *ClientTracker) OnSendBlock(block *protoblocktx.Block) {
	c.metrics.blockSentTotal.Add(1)
	c.metrics.transactionSentTotal.Add(len(block.Txs))
	c.latencyTracker.OnSendBlock(block)
}

// OnSendTransaction is a function that increments the transaction sent total and calls the latency tracker.
func (c *ClientTracker) OnSendTransaction(txID string) {
	c.metrics.transactionSentTotal.Add(1)
	c.latencyTracker.OnSendTransaction(txID)
}

func createProvider(c *metrics.Config) metrics.Provider {
	if !c.Enable {
		return metrics.NewNoOpProvider()
	}
	return &defaultProvider{
		LatencyConfig:  &c.Latency,
		Provider:       prometheusmetrics.NewProvider(),
		serverEndpoint: c.Endpoint,
	}
}

type defaultProvider struct {
	*metrics.LatencyConfig
	*prometheusmetrics.Provider
	serverEndpoint *connection.Endpoint
}

func (p *defaultProvider) StartPrometheusServer(ctx context.Context) error {
	return p.Provider.StartPrometheusServer(ctx, p.serverEndpoint)
}

func (p *defaultProvider) NewIntCounter(opts prometheus.CounterOpts) *metrics.IntCounter {
	return &metrics.IntCounter{Counter: p.NewCounter(opts)}
}

func (p *defaultProvider) NewIntGauge(opts prometheus.GaugeOpts) *metrics.IntGauge {
	return &metrics.IntGauge{Gauge: p.NewGauge(opts)}
}
