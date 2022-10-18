package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

var dbRequestSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "shard_db_request_size",
	Help: "Request size for read/write in the shard DB",
}, []string{"sub_component", "operation"})
var dbRequestLatency = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "shard_db_request_latency",
	Help: "Latency for read/write in the shard DB (ns)",
}, []string{"sub_component", "operation"})

type Metrics struct {
	Enabled                        bool
	IncomingTxs                    *metrics.ThroughputCounter
	CommittedSNs                   *metrics.ThroughputCounter
	PendingCommitsSNs              *metrics.InMemoryDataStructureGauge
	PendingCommitsTxIds            *metrics.InMemoryDataStructureGauge
	ShardInstanceTxShard           *metrics.InMemoryDataStructureGauge
	ShardInstanceTxResponse        *metrics.InMemoryDataStructureGauge
	SNReadDuration                 *prometheus.GaugeVec
	SNCommitDuration               *prometheus.GaugeVec
	SNReadSize                     *prometheus.GaugeVec
	SNCommitSize                   *prometheus.GaugeVec
	ShardsPhaseOneResponseChLength *metrics.ChannelBufferGauge
}

func New(enabled bool) *Metrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled:                 true,
		IncomingTxs:             metrics.NewThroughputCounterVec(metrics.In),
		CommittedSNs:            metrics.NewThroughputCounterVec(metrics.Out),
		PendingCommitsSNs:       metrics.NewInMemoryDataStructureGauge("pending_commits", "serial_numbers"),
		PendingCommitsTxIds:     metrics.NewInMemoryDataStructureGauge("pending_commits", "tx_ids"),
		ShardInstanceTxShard:    metrics.NewInMemoryDataStructureGauge("shard_instances", "tx_id_shard_id"),
		ShardInstanceTxResponse: metrics.NewInMemoryDataStructureGauge("shard_instances", "tx_id_response"),

		SNReadDuration:   dbRequestLatency.MustCurryWith(prometheus.Labels{"operation": "read"}),
		SNCommitDuration: dbRequestLatency.MustCurryWith(prometheus.Labels{"operation": "write"}),
		SNReadSize:       dbRequestSize.MustCurryWith(prometheus.Labels{"operation": "read"}),
		SNCommitSize:     dbRequestSize.MustCurryWith(prometheus.Labels{"operation": "write"}),

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
		m.PendingCommitsSNs,
		m.PendingCommitsTxIds,
		m.ShardInstanceTxShard,
		m.ShardInstanceTxResponse,
		m.ShardsPhaseOneResponseChLength,
		m.SNReadSize,
		m.SNCommitSize,
	}
}
