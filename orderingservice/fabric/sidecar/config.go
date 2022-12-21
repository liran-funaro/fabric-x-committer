package sidecar

import (
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

type SidecarConfig struct {
	Prometheus monitoring.Prometheus  `mapstructure:"prometheus"`
	Endpoint   connection.Endpoint    `mapstructure:"endpoint"`
	Orderer    *OrdererClientConfig   `mapstructure:"orderer"`
	Committer  *CommitterClientConfig `mapstructure:"committer"`
}

type OrdererClientConfig struct {
	ChannelID string              `mapstructure:"channel-id"`
	Endpoint  connection.Endpoint `mapstructure:"endpoint"`
}
type CommitterClientConfig struct {
	Endpoint              connection.Endpoint `mapstructure:"endpoint"`
	OutputChannelCapacity int                 `mapstructure:"output-channel-capacity"`
}

func ReadConfig() SidecarConfig {
	wrapper := new(struct {
		Config SidecarConfig `mapstructure:"sidecar"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("sidecar.endpoint", ":1234")

	viper.SetDefault("sidecar.orderer.channel-id", "mychannel")
	viper.SetDefault("sidecar.orderer.endpoint", ":7050")

	viper.SetDefault("sidecar.committer.endpoint", ":5002")
	viper.SetDefault("sidecar.committer.output-channel-capacity", 20)
}
