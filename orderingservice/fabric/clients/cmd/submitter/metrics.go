package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Provider struct {
}

func (p *Provider) ComponentName() string {
	return "submitter"
}
func (p *Provider) LatencyLabels() []string {
	return []string{}
}
func (p *Provider) NewMonitoring(bool, latency.AppTracer) metrics.AppMetrics {
	return &Metrics{
		metrics.NewThroughputCounter("submitter", metrics.Out),
	}
}

type Metrics struct {
	Throughput *metrics.ThroughputCounter
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	return []prometheus.Collector{m.Throughput}
}
