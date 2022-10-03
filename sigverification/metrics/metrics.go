package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Metrics struct {
	Enabled                        bool
	TxsReceived                    prometheus.Counter
	TxsSent                        prometheus.Counter
	BatchesReceived                prometheus.Counter
	BatchesSent                    prometheus.Counter
	ActiveStreams                  prometheus.Gauge
	ParallelExecutorInputChLength  *metrics.ChannelBufferGauge
	ParallelExecutorOutputChLength *metrics.ChannelBufferGauge
}

func New(enabled bool) *Metrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled: true,
		TxsReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "received_txs",
			Help: "The total number of processed TXs",
		}),
		TxsSent: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sent_txs",
			Help: "The total number of sent TXs",
		}),
		BatchesReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "received_batches",
			Help: "The total number of processed batches",
		}),
		BatchesSent: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sent_batches",
			Help: "The total number of sent batches",
		}),
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
	return []prometheus.Collector{m.TxsReceived, m.TxsSent, m.BatchesReceived, m.BatchesSent, m.ActiveStreams}
}
