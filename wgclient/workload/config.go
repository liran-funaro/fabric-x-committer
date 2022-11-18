package workload

import (
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

//type GeneratorLimitsConfig struct {
//	Parallelism       int    `mapstructure:"parallelism"`
//	ChannelBufferSize int    `mapstructure:"channel-buffer-size"`
//}

type GeneratorConfig struct {
	Profile string `mapstructure:"profile"`
	//Limits GeneratorLimitsConfig `mapstructre:"limits"`
}

type BlockgenStreamConfig struct {
	Prometheus monitoring.Prometheus `mapstructure:"prometheus"`
	Endpoint   connection.Endpoint   `mapstructure:"endpoint"`
	Generator  GeneratorConfig       `mapstructure:"generator"`
}

func ReadConfig() BlockgenStreamConfig {
	wrapper := new(struct {
		Config BlockgenStreamConfig `mapstructure:"blockgen"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("blockgen.generator.profile", "./profile-blockgen.yaml")
	//viper.SetDefault("blockgen.generator.limits.parallelism", 3)
	//viper.SetDefault("blockgen.generator.limits.channel-buffer-size", 10)

	viper.SetDefault("blockgen.endpoint", ":5002")
	viper.SetDefault("blockgen.prometheus.endpoint", ":2112")
	viper.SetDefault("blockgen.prometheus.latency-endpoint", ":14268")
}
