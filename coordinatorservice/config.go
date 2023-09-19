package coordinatorservice

import (
	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type (
	// CoordinatorConfig is the configuration for coordinator service. It contains configurations for all managers.
	CoordinatorConfig struct {
		ServerConfig                  *connection.ServerConfig  `mapstructure:"server"`
		SignVerifierConfig            *SignVerifierConfig       `mapstructure:"sign-verifier"`
		ValidatorCommitterConfig      *ValidatorCommitterConfig `mapstructure:"validator-committer"`
		DependencyGraphConfig         *DependencyGraphConfig    `mapstructure:"dependency-graph"`
		Monitoring                    *monitoring.Config        `mapstructure:"monitoring"`
		ChannelBufferSizePerGoroutine int                       `mapstructure:"per-channel-buffer-size-per-goroutine"`
	}

	// SignVerifierConfig is the configuration for signature verifier manager. It contains server endpoint for each
	// signature verifier server.
	SignVerifierConfig struct {
		ServerConfig []*connection.ServerConfig `mapstructure:"server"`
	}

	// DependencyGraphConfig is the configuration for dependency graph manager. It contains resource limits.
	DependencyGraphConfig struct {
		NumOfLocalDepConstructors       int `mapstructure:"num-of-local-dep-constructors"`
		WaitingTxsLimit                 int `mapstructure:"waiting-txs-limit"`
		NumOfWorkersForGlobalDepManager int `mapstructure:"num-of-workers-for-global-dep-manager"`
	}

	// ValidatorCommitterConfig is the configuration for validator committer manager. It contains server endpoint for
	// each validator committer server.
	ValidatorCommitterConfig struct {
		ServerConfig []*connection.ServerConfig `mapstructure:"server"`
	}
)

// ReadConfig reads the configuration from the viper instance.
// If the configuration file is used, the caller should call
// config.ReadFromYamlFile() before calling this function.
func ReadConfig() *CoordinatorConfig {
	setDefaults()

	wrapper := new(struct {
		Config CoordinatorConfig `mapstructure:"coordinator-service"`
	})
	config.Unmarshal(wrapper)
	return &wrapper.Config
}

func setDefaults() {
	viper.SetDefault("coordinator-service.server.endpoint.host", "localhost")
	viper.SetDefault("coordinator-service.server.endpoint.port", 3001)
	viper.SetDefault("coordinator-service.dependency-graph.num-of-local-dep-constructors", 5)
	viper.SetDefault("coordinator-service.dependency-graph.waiting-txs-limit", 20000)
	viper.SetDefault("coordinator-service.dependency-graph.num-of-workers-for-global-dep-manager", 10)
	viper.SetDefault("coordinator-service.per-channel-buffer-size-per-goroutine", 100)
}
