package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var IncomingTxs = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sc_shard_incoming_txs",
	Help: "The total number of processed TXs on phase 1",
}, []string{"shard_id"})

var CommittedSNs = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sc_shard_committed_sns",
	Help: "The total number of processed TXs on phase 2",
}, []string{"shard_id"})

var SNCommitDuration = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sc_shard_commit_duration",
	Help: "The total number of processed TXs on phase 1 entering phase 2",
}, []string{"shard_id"})

type ShardMetrics struct {
	IncomingTxs      *prometheus.CounterVec
	SNCommitDuration *prometheus.GaugeVec
	CommittedSNs     *prometheus.CounterVec
}

func NewShardMetrics(id uint32) *ShardMetrics {
	return &ShardMetrics{
		IncomingTxs:      IncomingTxs.MustCurryWith(shardId(id)),
		SNCommitDuration: SNCommitDuration.MustCurryWith(shardId(id)),
		CommittedSNs:     CommittedSNs.MustCurryWith(shardId(id)),
	}
}

func shardId(id uint32) prometheus.Labels {
	return prometheus.Labels{"shard_id": strconv.Itoa(int(id))}
}
