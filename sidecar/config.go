package sidecar

import (
	"time"

	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

type SidecarConfig struct {
	Monitoring monitoring.Config        `mapstructure:"monitoring"`
	Server     *connection.ServerConfig `mapstructure:"server"`
	Orderer    *OrdererClientConfig     `mapstructure:"orderer"`
	Committer  *CommitterClientConfig   `mapstructure:"committer"`
}

type OrdererClientConfig struct {
	ChannelID                string              `mapstructure:"channel-id"`
	Endpoint                 connection.Endpoint `mapstructure:"endpoint"`
	OrdererConnectionProfile string              `mapstructure:"orderer-connection-profile"`
	Reconnect                time.Duration       `mapstructure:"reconnect"`
}
type CommitterClientConfig struct {
	Endpoint              connection.Endpoint `mapstructure:"endpoint"`
	OutputChannelCapacity int                 `mapstructure:"output-channel-capacity"`
	LedgerPath            string              `mapstructure:"ledger-path"`
}

func ReadConfig() SidecarConfig {
	wrapper := new(struct {
		Config SidecarConfig `mapstructure:"sidecar"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("sidecar.server.endpoint", ":1234")
	viper.SetDefault("sidecar.monitoring.metrics.endpoint", ":2112")

	viper.SetDefault("sidecar.orderer.channel-id", "mychannel")
	viper.SetDefault("sidecar.orderer.endpoint", ":7050")
	viper.SetDefault("sidecar.orderer.reconnect", 10*time.Second)

	viper.SetDefault("sidecar.committer.endpoint", ":5002")
	viper.SetDefault("sidecar.committer.output-channel-capacity", 20)
	viper.SetDefault("sidecar.committer.ledger-path", "./ledger/")
}
