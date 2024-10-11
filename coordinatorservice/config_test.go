package coordinatorservice

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
)

func TestReadConfig(t *testing.T) {
	tests := []struct {
		name           string
		configFilePath string
		expectedConfig *CoordinatorConfig
	}{
		{
			name:           "config",
			configFilePath: "../config/samples/config-coordinator.yaml",
			expectedConfig: &CoordinatorConfig{
				ServerConfig: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 9001,
					},
				},
				SignVerifierConfig: &SignVerifierConfig{
					ServerConfig: []*connection.ServerConfig{
						{
							Endpoint: connection.Endpoint{
								Host: "localhost",
								Port: 4001,
							},
						},
						{
							Endpoint: connection.Endpoint{
								Host: "localhost",
								Port: 4002,
							},
						},
					},
				},
				ValidatorCommitterConfig: &ValidatorCommitterConfig{
					ServerConfig: []*connection.ServerConfig{
						{
							Endpoint: connection.Endpoint{
								Host: "localhost",
								Port: 5001,
							},
						},
						{
							Endpoint: connection.Endpoint{
								Host: "localhost",
								Port: 5002,
							},
						},
					},
				},
				DependencyGraphConfig: &DependencyGraphConfig{
					NumOfLocalDepConstructors:       20,
					WaitingTxsLimit:                 10000,
					NumOfWorkersForGlobalDepManager: 20,
				},
				ChannelBufferSizePerGoroutine: 300,
				Monitoring: &monitoring.Config{
					Metrics: &metrics.Config{
						Enable: true,
						Endpoint: &connection.Endpoint{
							Host: "",
							Port: 2110,
						},
					},
				},
			},
		},
		{
			name:           "default config",
			configFilePath: "./testdata/default-config.yaml",
			expectedConfig: &CoordinatorConfig{
				ServerConfig: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 3001,
					},
				},
				SignVerifierConfig: &SignVerifierConfig{
					ServerConfig: []*connection.ServerConfig{
						{
							Endpoint: connection.Endpoint{
								Host: "localhost",
								Port: 4001,
							},
						},
						{
							Endpoint: connection.Endpoint{
								Host: "localhost",
								Port: 4002,
							},
						},
					},
				},
				ValidatorCommitterConfig: &ValidatorCommitterConfig{
					ServerConfig: []*connection.ServerConfig{
						{
							Endpoint: connection.Endpoint{
								Host: "localhost",
								Port: 5001,
							},
						},
						{
							Endpoint: connection.Endpoint{
								Host: "localhost",
								Port: 5002,
							},
						},
					},
				},
				DependencyGraphConfig: &DependencyGraphConfig{
					NumOfLocalDepConstructors:       5,
					WaitingTxsLimit:                 20000,
					NumOfWorkersForGlobalDepManager: 10,
				},
				ChannelBufferSizePerGoroutine: 100,
			},
		},
	}

	for _, test := range tests {
		tt := test
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			require.NoError(t, config.ReadYamlConfigs([]string{tt.configFilePath}))
			c := ReadConfig()

			require.Equal(t, tt.expectedConfig, c)
		})
	}
}
