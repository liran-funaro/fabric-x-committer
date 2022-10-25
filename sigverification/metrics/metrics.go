package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Metrics struct {
	Enabled                        bool
	VerifierServerInTxs            *metrics.ThroughputCounter
	VerifierServerOutTxs           *metrics.ThroughputCounter
	ParallelExecutorInTxs          *metrics.ThroughputCounter
	ParallelExecutorOutTxs         *metrics.ThroughputCounter
	Latency                        *metrics.LatencyHistogram
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
		Latency:                metrics.NewDefaultLatencyHistogram("sigverifier_latency", 2*time.Millisecond, metrics.SampleThousandPerMillionUsing(metrics.TxSeqNumHasher)),
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
	return []prometheus.Collector{m.ActiveStreams,
		m.VerifierServerInTxs,
		m.VerifierServerOutTxs,
		m.ParallelExecutorInTxs,
		m.ParallelExecutorOutTxs,
		m.Latency,
		m.ParallelExecutorInputChLength,
		m.ParallelExecutorOutputChLength,
	}
}
