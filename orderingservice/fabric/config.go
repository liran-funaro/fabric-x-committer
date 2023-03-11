package fabric

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

type SubmitterConfig struct {
	Orderers                 []*connection.Endpoint `mapstructure:"orderers"`
	ChannelID                string                 `mapstructure:"channel-id"`
	OrdererConnectionProfile string                 `mapstructure:"orderer-connection-profile"`
	Monitoring               monitoring.Config      `mapstructure:"monitoring"`
	Messages                 int                    `mapstructure:"messages"`
	GoRoutines               int                    `mapstructure:"go-routines"`
	MessageSize              int                    `mapstructure:"message-size"`
	SignedEnvelopes          bool                   `mapstructure:"signed-envelopes"`
	RemoteControllerListener string                 `mapstructure:"remote-controller-listener"`
}

type ListenerConfig struct {
	Orderer                  connection.Endpoint `mapstructure:"orderer"`
	ChannelID                string              `mapstructure:"channel-id"`
	Monitoring               monitoring.Config   `mapstructure:"monitoring"`
	OrdererConnectionProfile string              `mapstructure:"orderer-connection-profile"`
}

func ReadListenerConfig() ListenerConfig {
	wrapper := new(struct {
		Config ListenerConfig `mapstructure:"orderer-listener"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func ReadSubmitterConfig() SubmitterConfig {
	wrapper := new(struct {
		Config SubmitterConfig `mapstructure:"orderer-submitter"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {

}
