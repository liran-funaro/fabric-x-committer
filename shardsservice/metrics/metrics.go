package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

var IncomingTxs = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "shard_incoming_txs",
	Help: "The total number of processed TXs on phase 1",
}, []string{"shard_id"})

var CommittedSNs = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "shard_committed_sns",
	Help: "The total number of processed TXs on phase 2",
}, []string{"shard_id"})

var SNCommitDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "shard_commit_duration",
	Help: "The total number of processed TXs on phase 1 entering phase 2",
}, []string{"shard_id"})

func ShardId(id uint32) prometheus.Labels {
	return prometheus.Labels{"shard_id": strconv.Itoa(int(id))}
}

var ShardsPhaseOneResponseChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "coordinator",
	Channel:      "phase_one_responses",
})

var AllMetrics = []prometheus.Collector{IncomingTxs, CommittedSNs, SNCommitDuration}
