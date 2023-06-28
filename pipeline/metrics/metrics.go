package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Metrics struct {
	Enabled bool

	RequestTracer latency.AppTracer

	SigVerifiedPendingTxs     prometheus.Gauge
	DependencyGraphPendingSNs prometheus.Gauge
	DependencyGraphPendingTXs prometheus.Gauge

	CoordinatorInTxs     *metrics.ThroughputCounter
	CoordinatorOutTxs    *metrics.ThroughputCounter
	SigVerifierMgrInTxs  *metrics.ThroughputCounter
	SigVerifierMgrOutTxs *metrics.ThroughputCounter
	DependencyMgrInTxs   *metrics.ThroughputCounter
	DependencyMgrOutTxs  *metrics.ThroughputCounter
	ShardMgrInTxs        *metrics.ThroughputCounter
	ShardMgrOutTxs       *metrics.ThroughputCounter
	PhaseOneInTxs        *metrics.ThroughputCounter
	PhaseOneOutTxs       *metrics.ThroughputCounter
	PhaseTwoInTxs        *metrics.ThroughputCounter
	PhaseTwoOutTxs       *metrics.ThroughputCounter

	DependencyMgrInputChLength          *metrics.ChannelBufferGauge
	DependencyMgrOutputChLength         *metrics.ChannelBufferGauge
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

const StatusLabel = "status"

type Provider struct {
}

func (p *Provider) ComponentName() string {
	return "coordinator"
}
func (p *Provider) LatencyLabels() []string {
	return []string{StatusLabel}
}
func (p *Provider) NewMonitoring(enabled bool, tracer latency.AppTracer) metrics.AppMetrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled:       true,
		RequestTracer: tracer,
		SigVerifiedPendingTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "coordinator_pending_txs",
			Help: "The total number of TXs with valid sigs, waiting on the dependency graph",
		}),
		DependencyGraphPendingSNs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dependency_graph_pending_sns",
			Help: "The size of the dependency graph in SNs",
		}),
		DependencyGraphPendingTXs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dependency_graph_pending_txs",
			Help: "The size of the dependency graph in TXs",
		}),

		// Throughput
		CoordinatorInTxs:     metrics.NewThroughputCounter("coordinator", metrics.In),
		CoordinatorOutTxs:    metrics.NewThroughputCounter("coordinator", metrics.Out),
		SigVerifierMgrInTxs:  metrics.NewThroughputCounter("sigverifier_mgr", metrics.In),
		SigVerifierMgrOutTxs: metrics.NewThroughputCounter("sigverifier_mgr", metrics.Out),
		DependencyMgrInTxs:   metrics.NewThroughputCounter("dependency_mgr", metrics.In),
		DependencyMgrOutTxs:  metrics.NewThroughputCounter("dependency_mgr", metrics.Out),
		ShardMgrInTxs:        metrics.NewThroughputCounter("shard_mgr", metrics.In),
		ShardMgrOutTxs:       metrics.NewThroughputCounter("shard_mgr", metrics.Out),
		PhaseOneInTxs:        metrics.NewThroughputCounter("phase_one", metrics.In),
		PhaseOneOutTxs:       metrics.NewThroughputCounter("phase_one", metrics.Out),
		PhaseTwoInTxs:        metrics.NewThroughputCounter("phase_two", metrics.In),
		PhaseTwoOutTxs:       metrics.NewThroughputCounter("phase_two", metrics.Out),

		// Channel Buffers
		DependencyMgrInputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "dep_mgr",
			Channel:      "input",
		}),
		DependencyMgrOutputChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "dep_mgr",
			Channel:      "output",
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
			Channel:      "phase_one_processed_txs",
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
	return []prometheus.Collector{
		m.SigVerifiedPendingTxs,
		m.DependencyGraphPendingSNs,
		m.DependencyGraphPendingTXs,
		m.CoordinatorInTxs,
		m.CoordinatorOutTxs,
		m.SigVerifierMgrInTxs,
		m.SigVerifierMgrOutTxs,
		m.DependencyMgrInTxs,
		m.DependencyMgrOutTxs,
		m.ShardMgrInTxs,
		m.ShardMgrOutTxs,
		m.PhaseOneInTxs,
		m.PhaseOneOutTxs,
		m.PhaseTwoInTxs,
		m.PhaseTwoOutTxs,
		m.DependencyMgrInputChLength,
		m.DependencyMgrOutputChLength,
		m.DependencyMgrStatusUpdateChLength,
		m.PhaseOneSendChLength,
		m.PhaseOneProcessedChLength,
		m.PhaseTwoSendChLength,
		m.ShardMgrInputChLength,
		m.ShardMgrOutputChLength,
		m.SigVerifierMgrInputChLength,
		m.SigVerifierMgrValidOutputChLength,
		m.SigVerifierMgrInvalidOutputChLength,
	}
}
