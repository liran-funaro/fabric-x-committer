package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

const ValidLabel = "valid"

var ValidStatusMap = map[bool]string{
	true:  "valid",
	false: "invalid",
}

type Metrics struct {
	Enabled                        bool
	RequestTracer                  latency.AppTracer
	VerifierServerInTxs            *metrics.ThroughputCounter
	VerifierServerOutTxs           *metrics.ThroughputCounter
	ParallelExecutorInTxs          *metrics.ThroughputCounter
	ParallelExecutorOutTxs         *metrics.ThroughputCounter
	ActiveStreams                  prometheus.Gauge
	ParallelExecutorInputChLength  *metrics.ChannelBufferGauge
	ParallelExecutorOutputChLength *metrics.ChannelBufferGauge
}

type Provider struct {
}

func (p *Provider) ComponentName() string {
	return "sigverifier"
}
func (p *Provider) LatencyLabels() []string {
	return []string{ValidLabel}
}
func (p *Provider) NewMonitoring(enabled bool, tracer latency.AppTracer) metrics.AppMetrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled:                true,
		RequestTracer:          tracer,
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

func (m *Metrics) AllMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		m.ActiveStreams,
		m.VerifierServerInTxs,
		m.VerifierServerOutTxs,
		m.ParallelExecutorInTxs,
		m.ParallelExecutorOutTxs,
		m.ParallelExecutorInputChLength,
		m.ParallelExecutorOutputChLength,
	}
}
