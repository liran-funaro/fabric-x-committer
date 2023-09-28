package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
)

const StatusLabel = "status"
const TxIdLabel = "txid"

type Metrics struct {
	Enabled bool

	TxStatusAggregatorSize prometheus.Gauge

	InTxs           *metrics.ThroughputCounter
	OutTxs          *metrics.ThroughputCounter
	CommitterOutTxs *metrics.ThroughputCounter
	CommitterInTxs  *metrics.ThroughputCounter

	OrdereredBlocksChLength *metrics.ChannelBufferGauge
}

type Provider struct {
}

func (p *Provider) ComponentName() string {
	return "sidecar"
}
func (p *Provider) LatencyLabels() []string {
	return []string{}
}
func (p *Provider) NewMonitoring(enabled bool) metrics.AppMetrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled: true,

		TxStatusAggregatorSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tx_status_aggregator",
			Help: "The total number of blocks in the status aggregator",
		}),

		// Throughput
		InTxs:           metrics.NewThroughputCounter("sidecar", metrics.In),
		OutTxs:          metrics.NewThroughputCounter("sidecar", metrics.Out),
		CommitterInTxs:  metrics.NewThroughputCounter("committer", metrics.In),
		CommitterOutTxs: metrics.NewThroughputCounter("committer", metrics.Out),

		// Channel Buffers
		OrdereredBlocksChLength: metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
			SubComponent: "sidecar",
			Channel:      "ordered_blocks",
		}),
	}
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		m.TxStatusAggregatorSize,
		m.InTxs,
		m.OutTxs,
		m.CommitterOutTxs,
		m.CommitterInTxs,
		m.OrdereredBlocksChLength,
	}
}
