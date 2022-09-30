package serverconfig

import (
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type SigVerificationConfig struct {
	Prometheus       connection.Prometheus   `mapstructure:"prometheus"`
	Endpoint         connection.Endpoint     `mapstructure:"endpoint"`
	Scheme           signature.Scheme        `mapstructure:"scheme"`
	ParallelExecutor parallelexecutor.Config `mapstructure:"parallel-executor"`
}

func (c *SigVerificationConfig) Connection() *connection.ServerConfig {
	return &connection.ServerConfig{Prometheus: c.Prometheus, Endpoint: c.Endpoint}
}

func ReadConfig() SigVerificationConfig {
	wrapper := new(struct {
		Config SigVerificationConfig `mapstructure:"sig-verification"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("sig-verification.endpoint", "localhost:5000")
	viper.SetDefault("sig-verification.prometheus.enabled", true)
	viper.SetDefault("sig-verification.prometheus.endpoint", "localhost:2112")

	viper.SetDefault("sig-verification.scheme", "Ecdsa")

	viper.SetDefault("sig-verification.parallel-executor.parallelism", 4)
	viper.SetDefault("sig-verification.parallel-executor.batch-time-cutoff", "500ms")
	viper.SetDefault("sig-verification.parallel-executor.batch-size-cutoff", 50)
	viper.SetDefault("sig-verification.parallel-executor.channel-buffer-size", 50)
}
