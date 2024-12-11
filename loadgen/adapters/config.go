package adapters

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters/broadcastclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type (
	// AdapterConfig contains all adapters configurations.
	AdapterConfig struct {
		VCClient          *VCClientConfig          `mapstructure:"vc-client"`
		CoordinatorClient *CoordinatorClientConfig `mapstructure:"coordinator-client"`
		SidecarClient     *SidecarClientConfig     `mapstructure:"sidecar-client"`
		SigVerifierClient *SVClientConfig          `mapstructure:"sig-verifier-client"`
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
