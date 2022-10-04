package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Metrics struct {
	Enabled                        bool
	IncomingTxs                    *prometheus.CounterVec
	CommittedSNs                   *prometheus.CounterVec
	SNCommitDuration               *prometheus.GaugeVec
	ShardsPhaseOneResponseChLength *metrics.ChannelBufferGauge
}

func New(enabled bool) *Metrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled:      true,
		IncomingTxs:  metrics.NewThroughputCounterVec(metrics.In),
		CommittedSNs: metrics.NewThroughputCounterVec(metrics.Out),

		SNCommitDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "shard_commit_duration",
			Help: "The total number of processed TXs on phase 1 entering phase 2",
		}, []string{"sub_component"}),

		ShardsPhaseOneResponseChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "shards_service",
			Channel:      "phase_one_responses",
		}),
	}
}

func ShardId(id uint32) prometheus.Labels {
	return prometheus.Labels{"sub_component": strconv.Itoa(int(id))}
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	if !m.Enabled {
		return []prometheus.Collector{}
	}
	return []prometheus.Collector{m.SNCommitDuration,
		m.IncomingTxs,
		m.CommittedSNs,
		m.SNCommitDuration,
		m.ShardsPhaseOneResponseChLength,
	}
}
