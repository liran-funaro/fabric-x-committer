package cmd

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"go.opentelemetry.io/otel/sdk/trace"
)

func LaunchSimpleThroughputMetrics(endpoint connection.Endpoint, subComponent string, direction metrics.ThroughputDirection) *Metrics {
	m := &Metrics{Throughput: metrics.NewThroughputCounter(subComponent, direction)}
	monitoring.LaunchPrometheus(monitoring.Prometheus{Endpoint: endpoint}, monitoring.Other, m)
	return m
}

type Metrics struct {
	Throughput *metrics.ThroughputCounter
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	return []prometheus.Collector{m.Throughput}
}
func (m *Metrics) IsEnabled() bool {
	return true
}
func (m *Metrics) SetTracerProvider(*trace.TracerProvider) {}
