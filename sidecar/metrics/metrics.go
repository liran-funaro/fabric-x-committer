package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"go.opentelemetry.io/otel/sdk/trace"
)

type Metrics struct {
	Enabled bool

	RequestTracer metrics.AppTracer

	TxStatusAggregatorSize prometheus.Gauge

	InTxs           *metrics.ThroughputCounter
	OutTxs          *metrics.ThroughputCounter
	CommitterOutTxs *metrics.ThroughputCounter
	CommitterInTxs  *metrics.ThroughputCounter

	OrdereredBlocksChLength *metrics.ChannelBufferGauge
}

func New(enabled bool) *Metrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled: true,

		RequestTracer: &metrics.NoopLatencyTracer{},

		TxStatusAggregatorSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tx_status_aggregator",
			Help: "The total number of blocks in the status aggregator",
		}),

		// Throughput
		InTxs:           metrics.NewThroughputCounter("sidecar", metrics.In),
		OutTxs:          metrics.NewThroughputCounter("sidecar", metrics.Out),
		CommitterInTxs:  metrics.NewThroughputCounter("committer", metrics.In),
		CommitterOutTxs: metrics.NewThroughputCounter("committer", metrics.Out),

		// Channel Buffers
		OrdereredBlocksChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "sidecar",
			Channel:      "ordered_blocks",
		}),
	}
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	if !m.Enabled {
		return []prometheus.Collector{}
	}
	return append(m.RequestTracer.Collectors(),
		m.TxStatusAggregatorSize,
		m.InTxs,
		m.OutTxs,
		m.CommitterOutTxs,
		m.CommitterInTxs,
		m.OrdereredBlocksChLength,
	)
}

func (m *Metrics) IsEnabled() bool {
	return m.Enabled
}

func (m *Metrics) SetTracerProvider(tp *trace.TracerProvider) {
	m.RequestTracer = metrics.NewDefaultLatencyTracer("sidecar_latency", 10*time.Second, tp)
}
