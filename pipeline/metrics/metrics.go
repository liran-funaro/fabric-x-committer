package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

var IncomingTxs = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "coordinator_incoming_txs",
	Help: "The total number of processed TXs on phase 1",
})

var SigVerifiedPendingTxs = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "coordinator_pending_txs",
	Help: "The total number of TXs with valid sigs, waiting on the dependency graph",
})

var ProcessedTxs = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "coordinator_resolved_txs",
	Help: "The total number of completely processed TXs",
})

var DependencyTotalSNs = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "dependency_graph_total_sns",
	Help: "The total number of SNs in the dependency graph",
})

var DependencyTotalTXs = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "dependency_graph_total_txs",
	Help: "The total number of TXs in the dependency graph",
})

// Channel Buffers

var DependencyMgrInputChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "dep_mgr",
	Channel:      "input",
})
var DependencyMgrStatusUpdateChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "dep_mgr",
	Channel:      "status_update",
})
var PhaseOneSendChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "shard_mgr",
	Channel:      "phase_one_send",
})
var PhaseOneProcessedChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "shard_mgr",
	Channel:      "phase_one_processed_txs_chan",
})
var PhaseTwoSendChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "shard_mgr",
	Channel:      "phase_two_send",
})
var ShardMgrInputChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "shard_mgr",
	Channel:      "input",
})
var ShardMgrOutputChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "shard_mgr",
	Channel:      "output",
})

var SigVerifierMgrInputChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "sig_mgr",
	Channel:      "input",
})
var SigVerifierMgrValidOutputChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "sig_mgr",
	Channel:      "valid_output",
})
var SigVerifierMgrInvalidOutputChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "sig_mgr",
	Channel:      "invalid_output",
})

var AllMetrics = []prometheus.Collector{IncomingTxs, SigVerifiedPendingTxs, ProcessedTxs, DependencyTotalSNs, DependencyTotalTXs}
