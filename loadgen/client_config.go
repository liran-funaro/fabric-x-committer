package loadgen

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type ClientConfig struct {
	VCClient          *VCClientConfig          `mapstructure:"vc-client"`
	CoordinatorClient *CoordinatorClientConfig `mapstructure:"coordinator-client"`
	SidecarClient     *SidecarClientConfig     `mapstructure:"sidecar-client"`
	SigVerifierClient *SVClientConfig          `mapstructure:"sig-verifier-client"`
	OrdererClient     *OrdererClientConfig     `mapstructure:"orderer-client"`

	Monitoring  *monitoring.Config `mapstructure:"monitoring"`
	RateLimit   LimiterConfig      `mapstructure:"rate-limit"`
	LoadProfile *Profile           `mapstructure:"load-profile"`
}

type SidecarClientConfig struct {
	Endpoint    *connection.Endpoint     `mapstructure:"endpoint"`
	Coordinator *CoordinatorClientConfig `mapstructure:"coordinator"`
	Orderer     OrdererClientConfig      `mapstructure:"orderer"`
}

type OrdererClientConfig struct {
	Endpoints         []*connection.Endpoint               `mapstructure:"endpoints"`
	ConnectionProfile *connection.OrdererConnectionProfile `mapstructure:"connection-profile"`
	SignedEnvelopes   bool                                 `mapstructure:"signed-envelopes"`
	Type              utils.ConsensusType                  `mapstructure:"type"`
	ChannelID         string                               `mapstructure:"channel-id"`
	Parallelism       int                                  `mapstructure:"parallelism"`
}

type CoordinatorClientConfig struct {
	Endpoint *connection.Endpoint `mapstructure:"endpoint"`
}

type VCClientConfig struct {
	Endpoints []*connection.Endpoint `mapstructure:"endpoints"`
}

type SVClientConfig struct {
	Endpoints []*connection.Endpoint `mapstructure:"endpoints"`
}

func ReadConfig() *ClientConfig {
	wrapper := new(ClientConfig)
	config.Unmarshal(wrapper)
	return wrapper
}
