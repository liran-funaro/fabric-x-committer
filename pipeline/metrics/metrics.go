package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var IncomingTxs = promauto.NewCounter(prometheus.CounterOpts{
	Name: "sc_coordinator_incoming_txs",
	Help: "The total number of processed TXs on phase 1",
})

var SigVerifiedPendingTxs = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "sc_coordinator_pending_txs",
	Help: "The total number of TXs with valid sigs, waiting on the dependency graph",
})

var ProcessedTxs = promauto.NewCounter(prometheus.CounterOpts{
	Name: "sc_coordinator_resolved_txs",
	Help: "The total number of completely processed TXs",
})

var DependencyTotalSNs = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "sc_dependency_graph_total_sns",
	Help: "The total number of SNs in the dependency graph",
})

var DependencyTotalTXs = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "sc_dependency_graph_total_txs",
	Help: "The total number of TXs in the dependency graph",
})

//var CommittedSNs = promauto.NewCounterVec(prometheus.CounterOpts{
//	Name: "sc_shard_committed_sns",
//	Help: "The total number of processed TXs on phase 2",
//}, []string{"shard_id"})
//
//var SNCommitDuration = promauto.NewGaugeVec(prometheus.GaugeOpts{
//	Name: "sc_shard_commit_duration",
//	Help: "The total number of processed TXs on phase 1 entering phase 2",
//}, []string{"shard_id"})
