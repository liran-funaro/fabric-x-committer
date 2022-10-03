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
		Enabled: true,
		IncomingTxs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shard_incoming_txs",
			Help: "The total number of processed TXs on phase 1",
		}, []string{"shard_id"}),

		CommittedSNs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shard_committed_sns",
			Help: "The total number of processed TXs on phase 2",
		}, []string{"shard_id"}),

		SNCommitDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "shard_commit_duration",
			Help: "The total number of processed TXs on phase 1 entering phase 2",
		}, []string{"shard_id"}),

		ShardsPhaseOneResponseChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "coordinator",
			Channel:      "phase_one_responses",
		}),
	}
}

func ShardId(id uint32) prometheus.Labels {
	return prometheus.Labels{"shard_id": strconv.Itoa(int(id))}
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	if !m.Enabled {
		return []prometheus.Collector{}
	}
	return []prometheus.Collector{m.IncomingTxs, m.CommittedSNs, m.SNCommitDuration}
}
