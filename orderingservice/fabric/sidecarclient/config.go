package sidecarclient

import (
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

type SidecarClientConfig struct {
	Orderers   []*connection.Endpoint `mapstructure:"orderers"`
	Committer  connection.Endpoint    `mapstructure:"committer"`
	Sidecar    connection.Endpoint    `mapstructure:"sidecar"`
	Prometheus monitoring.Prometheus  `mapstructure:"prometheus"`

	Profile              string `mapstructure:"profile"`
	InputChannelCapacity int    `mapstructure:"input-channel-capacity"`
	ChannelID            string `mapstructure:"channel-id"`
	Parallelism          int    `mapstructure:"parallelism"`
	SignedEnvelopes      bool   `mapstructure:"signed-envelopes"`
}

func ReadConfig() SidecarClientConfig {
	wrapper := new(struct {
		Config SidecarClientConfig `mapstructure:"sidecar-client"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("sidecar-client.sidecar", ":1234")
	viper.SetDefault("sidecar-client.committer", ":5002")
	viper.SetDefault("sidecar-client.orderers", []string{":7050"})
	viper.SetDefault("sidecar-client.prometheus.endpoint", ":2113")

	viper.SetDefault("sidecar-client.profile", "")
	viper.SetDefault("sidecar-client.input-channel-capacity", 20)
	viper.SetDefault("sidecar-client.channel-id", "mychannel")
	viper.SetDefault("sidecar-client.parallelism", 10)
	viper.SetDefault("sidecar-client.signed-envelopes", false)
}
