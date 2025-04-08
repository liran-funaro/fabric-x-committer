package main

import (
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/vc"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

//nolint:paralleltest // Cannot parallelize due to viper.
func TestConfig(t *testing.T) {
	tests := []struct {
		name                   string
		configFilePath         string
		expectedConfig         *vc.ValidatorCommitterServiceConfig
		expectedDataSourceName string
	}{
		{
			name:           "valid config",
			configFilePath: "../config/samples/config-vcservice.yaml",
			expectedConfig: &vc.ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6002,
					},
				},
				Database: &vc.DatabaseConfig{
					Endpoints: []*connection.Endpoint{
						connection.CreateEndpoint("localhost:5433"),
					},
					Username:       "yugabyte",
					Password:       "yugabyte",
					Database:       "yugabyte",
					MaxConnections: 10,
					MinConnections: 5,
					Retry: &connection.RetryProfile{
						MaxElapsedTime: 20 * time.Second,
					},
				},
				ResourceLimits: &vc.ResourceLimitsConfig{
					MaxWorkersForPreparer:             1,
					MaxWorkersForValidator:            1,
					MaxWorkersForCommitter:            20,
					MinTransactionBatchSize:           1,
					TimeoutForMinTransactionBatchSize: 2 * time.Second,
				},
				Monitoring: monitoring.Config{
					Server: &connection.ServerConfig{
						Endpoint: connection.Endpoint{
							Host: "localhost",
							Port: 2111,
						},
					},
				},
			},
			expectedDataSourceName: "postgres://yugabyte:yugabyte@localhost:5433/yugabyte?sslmode=disable",
		},
		{
			name:           "default config",
			configFilePath: "testdata/default_config.yaml",
			expectedConfig: &vc.ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6001,
					},
				},
				Database: &vc.DatabaseConfig{
					Endpoints: []*connection.Endpoint{
						connection.CreateEndpoint("localhost:5433"),
					},
					Username:       "yugabyte",
					Password:       "yugabyte",
					Database:       "yugabyte",
					MaxConnections: 20,
					MinConnections: 10,
					Retry: &connection.RetryProfile{
						MaxElapsedTime: 20 * time.Second,
					},
				},
				ResourceLimits: &vc.ResourceLimitsConfig{
					MaxWorkersForPreparer:             1,
					MaxWorkersForValidator:            1,
					MaxWorkersForCommitter:            20,
					MinTransactionBatchSize:           1,
					TimeoutForMinTransactionBatchSize: 5 * time.Second,
				},
				Monitoring: monitoring.Config{
					Server: &connection.ServerConfig{
						Endpoint: connection.Endpoint{
							Host: "localhost",
							Port: 6002,
						},
					},
				},
			},
			expectedDataSourceName: "postgres://yugabyte:yugabyte@localhost:5433/yugabyte?sslmode=disable",
		},
		{
			name: "config with multiple sources",
			configFilePath: config.CreateTempConfigFromTemplate(t, config.TemplateVC,
				&config.SystemConfig{
					ServerEndpoint:  connection.CreateEndpoint("localhost:6001"),
					MetricsEndpoint: connection.CreateEndpoint("localhost:6002"),
					Endpoints: config.SystemEndpoints{
						Database: []*connection.Endpoint{
							connection.CreateEndpoint("host1:1111"),
							connection.CreateEndpoint("host2:2222"),
						},
					},
					DB: config.DatabaseConfig{
						Name: "yugabyte",
					},
					Logging: &logging.Config{
						Enabled: false,
					},
				}),
			expectedConfig: &vc.ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6001,
					},
					Creds: nil,
				},
				Database: &vc.DatabaseConfig{
					Endpoints: []*connection.Endpoint{
						connection.CreateEndpoint("host1:1111"),
						connection.CreateEndpoint("host2:2222"),
					},
					Username:       "yugabyte",
					Password:       "yugabyte",
					Database:       "yugabyte",
					MaxConnections: 10,
					MinConnections: 5,
					Retry: &connection.RetryProfile{
						MaxElapsedTime: 20 * time.Second,
					},
				},
				ResourceLimits: &vc.ResourceLimitsConfig{
					MaxWorkersForPreparer:             1,
					MaxWorkersForValidator:            1,
					MaxWorkersForCommitter:            20,
					MinTransactionBatchSize:           1,
					TimeoutForMinTransactionBatchSize: 5 * time.Second,
				},
				Monitoring: monitoring.Config{
					Server: &connection.ServerConfig{
						Endpoint: connection.Endpoint{
							Host: "localhost",
							Port: 6002,
						},
					},
				},
			},
			expectedDataSourceName: "postgres://yugabyte:yugabyte@host1:1111,host2:2222/yugabyte?sslmode=disable",
		},
		{
			name:           "no config file",
			configFilePath: "",
			expectedConfig: &vc.ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6001,
					},
				},
				Database: &vc.DatabaseConfig{
					Endpoints: []*connection.Endpoint{
						connection.CreateEndpoint("localhost:5433"),
					},
					Username:       "yugabyte",
					Password:       "yugabyte",
					Database:       "yugabyte",
					MaxConnections: 20,
					MinConnections: 10,
					Retry: &connection.RetryProfile{
						MaxElapsedTime: 20 * time.Second,
					},
				},
				ResourceLimits: &vc.ResourceLimitsConfig{
					MaxWorkersForPreparer:             1,
					MaxWorkersForValidator:            1,
					MaxWorkersForCommitter:            20,
					MinTransactionBatchSize:           1,
					TimeoutForMinTransactionBatchSize: 5 * time.Second,
				},
				Monitoring: monitoring.Config{
					Server: &connection.ServerConfig{
						Endpoint: connection.Endpoint{
							Host: "localhost",
							Port: 6002,
						},
					},
				},
			},
			expectedDataSourceName: "postgres://yugabyte:yugabyte@localhost:5433/yugabyte?sslmode=disable",
		},
	}

	for _, test := range tests {
		tt := test
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			if tt.configFilePath != "" {
				require.NoError(t, config.ReadYamlConfigFile(tt.configFilePath))
			}

			c := readConfig()

			require.Equal(t, tt.expectedConfig, c)

			dataSourceName := c.Database.DataSourceName()
			require.Equal(t, tt.expectedDataSourceName, dataSourceName)
		})
	}
}
