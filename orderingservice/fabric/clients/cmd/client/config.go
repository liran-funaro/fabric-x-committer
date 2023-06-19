package main

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

type ClientConfig struct {
	Messages     int `mapstructure:"messages"`
	MessageSize  int `mapstructure:"message-size"`
	MessageStart int `mapstructure:"message-start"`

	Monitoring               monitoring.Config      `mapstructure:"monitoring"`
	Orderers                 []*connection.Endpoint `mapstructure:"orderers"`
	InputChannelCapacity     int                    `mapstructure:"input-channel-capacity"`
	ChannelID                string                 `mapstructure:"channel-id"`
	Parallelism              int                    `mapstructure:"parallelism"`
	SignedEnvelopes          bool                   `mapstructure:"signed-envelopes"`
	OrdererType              utils.ConsensusType    `mapstructure:"orderer-type"`
	OrdererConnectionProfile string                 `mapstructure:"orderer-connection-profile"`
	RemoteControllerListener connection.Endpoint    `mapstructure:"remote-controller-listener"`
}

func ReadClientConfig() ClientConfig {
	wrapper := new(struct {
		Config ClientConfig `mapstructure:"orderer-client"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {

}
