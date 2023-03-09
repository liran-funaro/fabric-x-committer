package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

const StatusLabel = "status"

type Provider struct {
}

func (p *Provider) ComponentName() string {
	return "shards-service"
}
func (p *Provider) LatencyLabels() []string {
	return []string{StatusLabel}
}
func (p *Provider) NewMonitoring(enabled bool, tracer latency.AppTracer) metrics.AppMetrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled:                 true,
		IncomingTxs:             metrics.NewThroughputCounterVec(metrics.In),
		CommittedSNs:            metrics.NewThroughputCounterVec(metrics.Out),
		RequestTracer:           tracer,
		PendingCommitsSNs:       metrics.NewInMemoryDataStructureGauge("pending_commits", "serial_numbers"),
		PendingCommitsTxIds:     metrics.NewInMemoryDataStructureGauge("pending_commits", "tx_ids"),
		ShardInstanceTxShard:    metrics.NewInMemoryDataStructureGauge("shard_instances", "tx_id_shard_id"),
		ShardInstanceTxResponse: metrics.NewInMemoryDataStructureGauge("shard_instances", "tx_id_response"),

		SNReadDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "shard_db_read_latency",
			Help:    "Latency for read/write in the shard DB (ns)",
			Buckets: utils.UniformBuckets(1000, 0, float64(1*time.Millisecond)),
		}, []string{"sub_component"}),
		SNCommitDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "shard_db_write_latency",
			Help:    "Latency for read/write in the shard DB (ns)",
			Buckets: utils.UniformBuckets(1000, 0, float64(100*time.Millisecond)),
		}, []string{"sub_component"}),
		SNReadSize:   dbRequestSize.MustCurryWith(prometheus.Labels{"operation": "read"}),
		SNCommitSize: dbRequestSize.MustCurryWith(prometheus.Labels{"operation": "write"}),

		ShardsPhaseOneResponseChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "shards_service",
			Channel:      "phase_one_responses",
		}),
	}
}

var dbRequestSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "shard_db_request_size",
	Help:    "Request size for read/write in the shard DB",
	Buckets: utils.UniformBuckets(1000, 0, 1000),
}, []string{"sub_component", "operation"})

type Metrics struct {
	Enabled                        bool
	IncomingTxs                    *metrics.ThroughputCounter
	CommittedSNs                   *metrics.ThroughputCounter
	RequestTracer                  latency.AppTracer
	PendingCommitsSNs              *metrics.InMemoryDataStructureGauge
	PendingCommitsTxIds            *metrics.InMemoryDataStructureGauge
	ShardInstanceTxShard           *metrics.InMemoryDataStructureGauge
	ShardInstanceTxResponse        *metrics.InMemoryDataStructureGauge
	SNReadDuration                 prometheus.ObserverVec
	SNCommitDuration               prometheus.ObserverVec
	SNReadSize                     prometheus.ObserverVec
	SNCommitSize                   prometheus.ObserverVec
	ShardsPhaseOneResponseChLength *metrics.ChannelBufferGauge
}

func ShardId(id uint32) prometheus.Labels {
	return prometheus.Labels{"sub_component": strconv.Itoa(int(id))}
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		m.SNReadDuration,
		m.SNCommitDuration,
		m.IncomingTxs,
		m.CommittedSNs,
		m.PendingCommitsSNs,
		m.PendingCommitsTxIds,
		m.ShardInstanceTxShard,
		m.ShardInstanceTxResponse,
		m.ShardsPhaseOneResponseChLength,
	}
}
