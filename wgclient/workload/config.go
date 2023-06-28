package workload

import (
	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
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
	Monitoring monitoring.Config   `mapstructure:"monitoring"`
	Endpoint   connection.Endpoint `mapstructure:"endpoint"`
	Generator  GeneratorConfig     `mapstructure:"generator"`
}

func ReadConfig(filepaths []string) BlockgenStreamConfig {
	config.ReadYamlConfigs(filepaths)
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
	viper.SetDefault("blockgen.monitoring.metrics.endpoint", ":2112")
	viper.SetDefault("blockgen.monitoring.latency.endpoint", ":14268")
}
