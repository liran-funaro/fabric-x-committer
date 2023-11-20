package sidecarservice

import (
	"time"

	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/coordinatorclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type SidecarConfig struct {
	Monitoring *monitoring.Config        `mapstructure:"monitoring"`
	Server     *connection.ServerConfig  `mapstructure:"server"`
	Orderer    *deliverclient.Config     `mapstructure:"orderer"`
	Committer  *coordinatorclient.Config `mapstructure:"committer"`
	Ledger     *LedgerConfig             `mapstructure:"ledger"`
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
