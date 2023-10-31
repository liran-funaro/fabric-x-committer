package sidecarservice

import (
	"time"

	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type SidecarConfig struct {
	Monitoring *monitoring.Config       `mapstructure:"monitoring"`
	Server     *connection.ServerConfig `mapstructure:"server"`
	Orderer    *OrdererClientConfig     `mapstructure:"orderer"`
	Committer  *CommitterClientConfig   `mapstructure:"committer"`
	Ledger     *LedgerConfig            `mapstructure:"ledger"`
}

type OrdererClientConfig struct {
	ChannelID                string                               `mapstructure:"channel-id"`
	Endpoint                 connection.Endpoint                  `mapstructure:"endpoint"`
	OrdererConnectionProfile *connection.OrdererConnectionProfile `mapstructure:"orderer-connection-profile"`
	Reconnect                time.Duration                        `mapstructure:"reconnect"`
}
type CommitterClientConfig struct {
	Endpoint connection.Endpoint `mapstructure:"endpoint"`
}

type LedgerConfig struct {
	Path string `mapstructure:"path"`
}

func ReadConfig() SidecarConfig {
	wrapper := new(struct {
		Config SidecarConfig `mapstructure:"sidecar"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("sidecar.server.endpoint", ":8832")
	viper.SetDefault("sidecar.metrics.endpoint", ":2112")

	viper.SetDefault("sidecar.orderer.channel-id", "mychannel")
	viper.SetDefault("sidecar.orderer.endpoint", ":7050")
	viper.SetDefault("sidecar.orderer.reconnect", 10*time.Second)

	viper.SetDefault("sidecar.committer.endpoint", ":5002")
	viper.SetDefault("sidecar.committer.output-channel-capacity", 20)

	viper.SetDefault("sidecar.ledger.path", "./ledger/")
}
