package loadgen

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/broadcastclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

// ClientConfig is a struct that contains the configuration for the client.
type ClientConfig struct {
	VCClient          *VCClientConfig          `mapstructure:"vc-client"`
	CoordinatorClient *CoordinatorClientConfig `mapstructure:"coordinator-client"`
	SidecarClient     *SidecarClientConfig     `mapstructure:"sidecar-client"`
	SigVerifierClient *SVClientConfig          `mapstructure:"sig-verifier-client"`

	Monitoring  *monitoring.Config `mapstructure:"monitoring"`
	RateLimit   LimiterConfig      `mapstructure:"rate-limit"`
	LoadProfile *Profile           `mapstructure:"load-profile"`
}

// SidecarClientConfig is a struct that contains the configuration for the sidecar client.
type SidecarClientConfig struct {
	Endpoint    *connection.Endpoint     `mapstructure:"endpoint"`
	Coordinator *CoordinatorClientConfig `mapstructure:"coordinator"`
	Orderer     broadcastclient.Config   `mapstructure:"orderer"`
}

// CoordinatorClientConfig is a struct that contains the configuration for the coordinator client.
type CoordinatorClientConfig struct {
	Endpoint *connection.Endpoint `mapstructure:"endpoint"`
}

// VCClientConfig is a struct that contains the configuration for the VC client.
type VCClientConfig struct {
	Endpoints []*connection.Endpoint `mapstructure:"endpoints"`
}

// SVClientConfig is a struct that contains the configuration for the signature verifier client.
type SVClientConfig struct {
	Endpoints []*connection.Endpoint `mapstructure:"endpoints"`
}

// ReadConfig is a function that reads the client configuration.
func ReadConfig() *ClientConfig {
	wrapper := new(ClientConfig)
	config.Unmarshal(wrapper)
	return wrapper
}
