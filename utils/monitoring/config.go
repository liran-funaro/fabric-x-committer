package monitoring

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Config struct {
	Metrics *metrics.Config `mapstructure:"metrics"`
	Latency *latency.Config `mapstructure:"latency"`
}

func (p *Config) IsMetricsEnabled() bool {
	return p.Metrics != nil
}

type Provider interface {
	ComponentName() string
	LatencyLabels() []string
	NewMonitoring(bool, latency.AppTracer) metrics.AppMetrics
}
