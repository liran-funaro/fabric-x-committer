package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"go.opentelemetry.io/otel/sdk/trace"
)

type Metrics struct {
	Enabled                        bool
	RequestTracer                  metrics.AppTracer
	VerifierServerInTxs            *metrics.ThroughputCounter
	VerifierServerOutTxs           *metrics.ThroughputCounter
	ParallelExecutorInTxs          *metrics.ThroughputCounter
	ParallelExecutorOutTxs         *metrics.ThroughputCounter
	ActiveStreams                  prometheus.Gauge
	ParallelExecutorInputChLength  *metrics.ChannelBufferGauge
	ParallelExecutorOutputChLength *metrics.ChannelBufferGauge
}

func New(enabled bool) *Metrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled:                true,
		RequestTracer:          &metrics.NoopLatencyTracer{},
		VerifierServerInTxs:    metrics.NewThroughputCounter("verifier_server", metrics.In),
		VerifierServerOutTxs:   metrics.NewThroughputCounter("verifier_server", metrics.Out),
		ParallelExecutorInTxs:  metrics.NewThroughputCounter("parallel_executor", metrics.In),
		ParallelExecutorOutTxs: metrics.NewThroughputCounter("parallel_executor", metrics.Out),
		ActiveStreams: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "active_streams",
			Help: "The total number of started streams",
		}),

		ParallelExecutorInputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "parallel_executor",
			Channel:      "input",
		}),
		ParallelExecutorOutputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "parallel_executor",
			Channel:      "output",
		}),
	}
}

func (m *Metrics) IsEnabled() bool {
	return m.Enabled
}

func (m *Metrics) SetTracerProvider(tp *trace.TracerProvider) {
	m.RequestTracer = metrics.NewDefaultLatencyTracer("sigverifier_latency", 1*time.Second, tp)
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	if !m.Enabled {
		return []prometheus.Collector{}
	}
	return append(m.RequestTracer.Collectors(),
		m.ActiveStreams,
		m.VerifierServerInTxs,
		m.VerifierServerOutTxs,
		m.ParallelExecutorInTxs,
		m.ParallelExecutorOutTxs,
		m.ParallelExecutorInputChLength,
		m.ParallelExecutorOutputChLength)
}
