package metrics

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Metrics struct {
	Enabled bool

	SigVerifiedPendingTxs     prometheus.Gauge
	DependencyGraphPendingSNs prometheus.Gauge
	DependencyGraphPendingTXs prometheus.Gauge

	//PreSignatureLatency  *metrics.LatencyHistogram
	//SignatureLatency     *metrics.LatencyHistogram
	//PostSignatureLatency *metrics.LatencyHistogram
	//PrePhaseOneLatency   *metrics.LatencyHistogram
	WaitingPhaseOneIn    prometheus.Histogram
	WaitingPhaseOneOut   prometheus.Histogram
	WaitingDepMgrIn      prometheus.Histogram
	WaitingDepMgrOut     prometheus.Histogram
	WaitingSigVerMgrIn   prometheus.Histogram
	WaitingSigVerMgrOut  prometheus.Histogram
	PhaseOneLatency      *metrics.LatencyHistogram
	StatusProcessLatency *metrics.LatencyHistogram

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
		//PreSignatureLatency:  metrics.NewDefaultLatencyHistogram("pre_signature_latency", 5*time.Second, metrics.SampleThousandPerMillionUsing(metrics.Identity)),
		//SignatureLatency:     metrics.NewDefaultLatencyHistogram("signature_latency", 5*time.Second, metrics.SampleThousandPerMillionUsing(metrics.Identity)),
		//PostSignatureLatency: metrics.NewDefaultLatencyHistogram("post_signature_latency", 5*time.Second, metrics.SampleThousandPerMillionUsing(metrics.TxSeqNumHasher)),
		//PrePhaseOneLatency:   metrics.NewDefaultLatencyHistogram("pre_phase_one_latency", 5*time.Second, metrics.SampleThousandPerMillionUsing(metrics.TxSeqNumHasher)),
		WaitingPhaseOneIn:    WaitingChannelHistogram("phase_one", "in"),
		WaitingPhaseOneOut:   WaitingChannelHistogram("phase_one", "out"),
		WaitingDepMgrIn:      WaitingChannelHistogram("dep_mgr", "in"),
		WaitingDepMgrOut:     WaitingChannelHistogram("dep_mgr", "out"),
		WaitingSigVerMgrIn:   WaitingChannelHistogram("sig_ver_mgr", "in"),
		WaitingSigVerMgrOut:  WaitingChannelHistogram("sig_ver_mgr", "out"),
		PhaseOneLatency:      metrics.NewDefaultLatencyHistogram("phase_one_latency", 5*time.Second, metrics.SampleThousandPerMillionUsing(metrics.TxSeqNumHasher)),
		StatusProcessLatency: metrics.NewDefaultLatencyHistogram("status_process_latency", 5*time.Second, metrics.SampleThousandPerMillionUsing(metrics.TxSeqNumHasher)),
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

func WaitingChannelHistogram(channelName, direction string) prometheus.Histogram {
	return prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("waiting_%s_%s_latency", channelName, direction),
		Help:    "Latency for read/write in the channel (ns)",
		Buckets: metrics.UniformBuckets(1000, 0, float64(5*time.Second)),
	})
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	if !m.Enabled {
		return []prometheus.Collector{}
	}
	return []prometheus.Collector{m.SigVerifiedPendingTxs, m.DependencyGraphPendingSNs, m.DependencyGraphPendingTXs,
		m.CoordinatorInTxs,
		m.CoordinatorOutTxs,
		//m.PreSignatureLatency,
		//m.SignatureLatency,
		//m.PostSignatureLatency,
		//m.PrePhaseOneLatency,
		m.WaitingPhaseOneIn,
		m.WaitingPhaseOneOut,
		m.WaitingDepMgrIn,
		m.WaitingDepMgrOut,
		m.WaitingSigVerMgrIn,
		m.WaitingSigVerMgrOut,
		m.PhaseOneLatency,
		m.StatusProcessLatency,
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
