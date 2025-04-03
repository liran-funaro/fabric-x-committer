package main

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/coordinator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

//nolint:paralleltest // Cannot parallelize due to viper.
func TestReadConfig(t *testing.T) {
	tests := []struct {
		name           string
		configFilePath string
		expectedConfig *coordinator.Config
	}{
		{
			name:           "config",
			configFilePath: "../config/samples/config-coordinator.yaml",
			expectedConfig: &coordinator.Config{
				ServerConfig: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 9001,
					},
				},
				SignVerifierConfig: &coordinator.SignVerifierConfig{
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
				ValidatorCommitterConfig: &coordinator.ValidatorCommitterConfig{
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
				DependencyGraphConfig: &coordinator.DependencyGraphConfig{
					NumOfLocalDepConstructors:       1,
					WaitingTxsLimit:                 10000,
					NumOfWorkersForGlobalDepManager: 1,
				},
				ChannelBufferSizePerGoroutine: 10,
				Monitoring: monitoring.Config{
					Server: &connection.ServerConfig{
						Endpoint: connection.Endpoint{
							Host: "localhost",
							Port: 2110,
						},
					},
				},
			},
		},
		{
			name:           "default config",
			configFilePath: "./testdata/default-config.yaml",
			expectedConfig: &coordinator.Config{
				ServerConfig: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 3001,
					},
				},
				SignVerifierConfig: &coordinator.SignVerifierConfig{
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
				ValidatorCommitterConfig: &coordinator.ValidatorCommitterConfig{
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
				DependencyGraphConfig: &coordinator.DependencyGraphConfig{
					NumOfLocalDepConstructors:       1,
					WaitingTxsLimit:                 10000,
					NumOfWorkersForGlobalDepManager: 1,
				},
				ChannelBufferSizePerGoroutine: 10,
				Monitoring: monitoring.Config{
					Server: &connection.ServerConfig{
						Endpoint: *connection.CreateEndpoint("localhost:7005"),
					},
				},
			},
		},
	}

	for _, test := range tests {
		tt := test
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			require.NoError(t, config.ReadYamlConfigFile(tt.configFilePath))
			c := readConfig()
			require.Equal(t, tt.expectedConfig, c)
		})
	}
}
