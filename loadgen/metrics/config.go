package metrics

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

// Config describes the load generator metrics.
// It adds latency tracker to the common metrics configurations.
type Config struct {
	monitoring.Config `mapstructure:",squash" yaml:",inline"`
	Latency           LatencyConfig `mapstructure:"latency" yaml:"latency"`
}
