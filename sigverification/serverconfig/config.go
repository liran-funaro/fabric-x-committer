package serverconfig

import (
	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type SigVerificationConfig struct {
	Monitoring       monitoring.Config        `mapstructure:"monitoring"`
	Server           *connection.ServerConfig `mapstructure:"server"`
	Scheme           signature.Scheme         `mapstructure:"scheme"`
	ParallelExecutor parallelexecutor.Config  `mapstructure:"parallel-executor"`
}

func ReadConfig() SigVerificationConfig {
	wrapper := new(struct {
		Config SigVerificationConfig `mapstructure:"sig-verification"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("sig-verification.server.endpoint", ":5000")
	viper.SetDefault("sig-verification.monitoring.metrics.endpoint", ":2112")
	viper.SetDefault("sig-verification.monitoring.latency.endpoint", ":14268")
	viper.SetDefault("sig-verification.monitoring.latency.span-exporter", "console")
	viper.SetDefault("sig-verification.monitoring.latency.sampler.type", "never")

	viper.SetDefault("sig-verification.scheme", "Ecdsa")

	viper.SetDefault("sig-verification.parallel-executor.parallelism", 4)
	viper.SetDefault("sig-verification.parallel-executor.batch-time-cutoff", "500ms")
	viper.SetDefault("sig-verification.parallel-executor.batch-size-cutoff", 50)
	viper.SetDefault("sig-verification.parallel-executor.channel-buffer-size", 50)
}
