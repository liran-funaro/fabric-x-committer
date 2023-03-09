package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Provider struct {
}

func (p *Provider) ComponentName() string {
	return "listener"
}
func (p *Provider) LatencyLabels() []string {
	return []string{}
}
func (p *Provider) NewMonitoring(bool, latency.AppTracer) metrics.AppMetrics {
	return &Metrics{
		metrics.NewThroughputCounter("listener", metrics.In),
		prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "block_sizes",
			Help: "Sizes of incoming blocks",
		}, []string{"sub_component"}).With(prometheus.Labels{"sub_component": "listener"}),
	}
}

type Metrics struct {
	Throughput *metrics.ThroughputCounter
	BlockSizes prometheus.Gauge
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	return []prometheus.Collector{m.Throughput, m.BlockSizes}
}
