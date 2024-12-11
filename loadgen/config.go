package loadgen

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type (
	// ClientConfig is a struct that contains the configuration for the client.
	ClientConfig struct {
		Adapter adapters.AdapterConfig `mapstructure:",squash"`

		Monitoring *monitoring.Config `mapstructure:"monitoring"`
		BufferSize int                `mapstructure:"buffer-size"`

		LoadProfile *workload.Profile       `mapstructure:"load-profile"`
		Stream      *workload.StreamOptions `mapstructure:"stream"`

		OnlyLoadGeneration      bool `mapstructure:"only-load-generation"`
		OnlyNamespaceGeneration bool `mapstructure:"only-namespace-generation"`
	}
)

// ReadConfig is a function that reads the client configuration.
func ReadConfig() *ClientConfig {
	wrapper := new(ClientConfig)
	config.Unmarshal(wrapper)
	return wrapper
}
