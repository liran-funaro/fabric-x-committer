package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Metrics struct {
	Enabled                        bool
	VerifierServerInTxs            prometheus.Counter
	VerifierServerOutTxs           prometheus.Counter
	ParallelExecutorInTxs          prometheus.Counter
	ParallelExecutorOutTxs         prometheus.Counter
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
	if !m.Enabled {
		return []prometheus.Collector{}
	}
	return []prometheus.Collector{m.ActiveStreams}
}
