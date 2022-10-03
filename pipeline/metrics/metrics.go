package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Metrics struct {
	Enabled                             bool
	IncomingTxs                         prometheus.Counter
	SigVerifiedPendingTxs               prometheus.Gauge
	ProcessedTxs                        prometheus.Counter
	DependencyTotalSNs                  prometheus.Gauge
	DependencyTotalTXs                  prometheus.Gauge
	DependencyMgrInputChLength          *metrics.ChannelBufferGauge
	DependencyMgrStatusUpdateChLength   *metrics.ChannelBufferGauge
	PhaseOneSendChLength                *metrics.ChannelBufferGauge
	PhaseOneProcessedChLength           *metrics.ChannelBufferGauge
	PhaseTwoSendChLength                *metrics.ChannelBufferGauge
	ShardMgrInputChLength               *metrics.ChannelBufferGauge
	ShardMgrOutputChLength              *metrics.ChannelBufferGauge
	SigVerifierMgrInputChLength         *metrics.ChannelBufferGauge
	SigVerifierMgrValidOutputChLength   *metrics.ChannelBufferGauge
	SigVerifierMgrInvalidOutputChLength *metrics.ChannelBufferGauge
}

func New(enabled bool) *Metrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled: true,
		IncomingTxs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "coordinator_incoming_txs",
			Help: "The total number of processed TXs on phase 1",
		}),

		SigVerifiedPendingTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "coordinator_pending_txs",
			Help: "The total number of TXs with valid sigs, waiting on the dependency graph",
		}),

		ProcessedTxs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "coordinator_resolved_txs",
			Help: "The total number of completely processed TXs",
		}),

		DependencyTotalSNs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dependency_graph_total_sns",
			Help: "The total number of SNs in the dependency graph",
		}),

		DependencyTotalTXs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dependency_graph_total_txs",
			Help: "The total number of TXs in the dependency graph",
		}),

		// Channel Buffers

		DependencyMgrInputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "dep_mgr",
			Channel:      "input",
		}),
		DependencyMgrStatusUpdateChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "dep_mgr",
			Channel:      "status_update",
		}),
		PhaseOneSendChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "shard_mgr",
			Channel:      "phase_one_send",
		}),
		PhaseOneProcessedChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "shard_mgr",
			Channel:      "phase_one_processed_txs_chan",
		}),
		PhaseTwoSendChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "shard_mgr",
			Channel:      "phase_two_send",
		}),
		ShardMgrInputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "shard_mgr",
			Channel:      "input",
		}),
		ShardMgrOutputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "shard_mgr",
			Channel:      "output",
		}),

		SigVerifierMgrInputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "sig_mgr",
			Channel:      "input",
		}),
		SigVerifierMgrValidOutputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "sig_mgr",
			Channel:      "valid_output",
		}),
		SigVerifierMgrInvalidOutputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "sig_mgr",
			Channel:      "invalid_output",
		}),
	}
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	if !m.Enabled {
		return []prometheus.Collector{}
	}
	return []prometheus.Collector{m.IncomingTxs, m.SigVerifiedPendingTxs, m.ProcessedTxs, m.DependencyTotalSNs, m.DependencyTotalTXs}
}
