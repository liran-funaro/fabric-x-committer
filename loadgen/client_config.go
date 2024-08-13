package loadgen

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/broadcastclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type (
	// ClientConfig is a struct that contains the configuration for the client.
	ClientConfig struct {
		VCClient          *VCClientConfig          `mapstructure:"vc-client"`
		CoordinatorClient *CoordinatorClientConfig `mapstructure:"coordinator-client"`
		SidecarClient     *SidecarClientConfig     `mapstructure:"sidecar-client"`
		SigVerifierClient *SVClientConfig          `mapstructure:"sig-verifier-client"`

		Monitoring *monitoring.Config `mapstructure:"monitoring"`
		BufferSize int                `mapstructure:"buffer-size"`

		LoadProfile *Profile       `mapstructure:"load-profile"`
		Stream      *StreamOptions `mapstructure:"stream"`
	}

	// SidecarClientConfig is a struct that contains the configuration for the sidecar client.
	SidecarClientConfig struct {
		Endpoint    *connection.Endpoint     `mapstructure:"endpoint"`
		Coordinator *CoordinatorClientConfig `mapstructure:"coordinator"`
		Orderer     broadcastclient.Config   `mapstructure:"orderer"`
	}

	// CoordinatorClientConfig is a struct that contains the configuration for the coordinator client.
	CoordinatorClientConfig struct {
		Endpoint *connection.Endpoint `mapstructure:"endpoint"`
	}

	// VCClientConfig is a struct that contains the configuration for the VC client.
	VCClientConfig struct {
		Endpoints []*connection.Endpoint `mapstructure:"endpoints"`
	}

	// SVClientConfig is a struct that contains the configuration for the signature verifier client.
	SVClientConfig struct {
		Endpoints []*connection.Endpoint `mapstructure:"endpoints"`
	}
)

// ReadConfig is a function that reads the client configuration.
func ReadConfig() *ClientConfig {
	wrapper := new(ClientConfig)
	config.Unmarshal(wrapper)
	return wrapper
}
