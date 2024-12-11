package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	promgo "github.com/prometheus/client_model/go"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
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

	latencyTracker ReceiverSender
}

// NewLoadgenServiceMetrics creates a new PerfMetrics instance.
func NewLoadgenServiceMetrics(p metrics.Provider) *PerfMetrics {
	buckets := p.Buckets()
	m := &PerfMetrics{
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
	m.latencyTracker = NewReceiverSender(
		m.provider, m.validTransactionLatencySecond, m.invalidTransactionLatencySecond,
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
func (c *PerfMetrics) OnSendBlock(block *protoblocktx.Block) {
	c.blockSentTotal.Add(1)
	c.transactionSentTotal.Add(len(block.Txs))
	c.latencyTracker.OnSendBlock(block)
}

// OnSendTransaction is a function that increments the transaction sent total and calls the latency tracker.
func (c *PerfMetrics) OnSendTransaction(txID string) {
	c.transactionSentTotal.Add(1)
	c.latencyTracker.OnSendTransaction(txID)
}

// CreateProvider creates the appropriate provider for the metrics.
func CreateProvider(c *metrics.Config) metrics.Provider {
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
