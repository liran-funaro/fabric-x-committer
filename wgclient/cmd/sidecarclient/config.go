package main

import (
	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type Config struct {
	Committer connection.Endpoint `mapstructure:"committer"`
	Sidecar   connection.Endpoint `mapstructure:"sidecar"`
	Profile   string              `mapstructure:"profile"`

	Monitoring               monitoring.Config                   `mapstructure:"monitoring"`
	Orderers                 []*connection.Endpoint              `mapstructure:"orderers"`
	InputChannelCapacity     int                                 `mapstructure:"input-channel-capacity"`
	ChannelID                string                              `mapstructure:"channel-id"`
	Parallelism              int                                 `mapstructure:"parallelism"`
	SignedEnvelopes          bool                                `mapstructure:"signed-envelopes"`
	OrdererType              utils.ConsensusType                 `mapstructure:"orderer-type"`
	OrdererConnectionProfile connection.OrdererConnectionProfile `mapstructure:"orderer-connection-profile"`
	RemoteControllerListener connection.Endpoint                 `mapstructure:"remote-controller-listener"`
	InitialRateLimit         int                                 `mapstructure:"initial-rate-limit"`
}

func ReadConfig() Config {
	wrapper := new(struct {
		Config Config `mapstructure:"sidecar-client"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("sidecar-client.sidecar", ":1234")
	viper.SetDefault("sidecar-client.committer", ":5002")
	viper.SetDefault("sidecar-client.orderers", []string{":7050"})
	viper.SetDefault("sidecar-client.monitoring.metrics.endpoint", ":2113")

	viper.SetDefault("sidecar-client.profile", "")
	viper.SetDefault("sidecar-client.input-channel-capacity", 20)
	viper.SetDefault("sidecar-client.channel-id", "mychannel")
	viper.SetDefault("sidecar-client.parallelism", 10)
	viper.SetDefault("sidecar-client.signed-envelopes", false)
}
