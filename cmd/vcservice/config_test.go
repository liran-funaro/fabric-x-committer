package main

import (
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	configtempl "github.ibm.com/decentralized-trust-research/scalable-committer/config/templates"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

func TestConfig(t *testing.T) {
	tests := []struct {
		name                   string
		configFilePath         string
		expectedConfig         *vcservice.ValidatorCommitterServiceConfig
		expectedDataSourceName string
	}{
		{
			name:           "valid config",
			configFilePath: "../../config/samples/config-vcservice.yaml",
			expectedConfig: &vcservice.ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6002,
					},
				},
				Database: &vcservice.DatabaseConfig{
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
				ResourceLimits: &vcservice.ResourceLimitsConfig{
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
			expectedConfig: &vcservice.ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6001,
					},
				},
				Database: &vcservice.DatabaseConfig{
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
				ResourceLimits: &vcservice.ResourceLimitsConfig{
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
			configFilePath: runner.CreateConfigFromTemplate(t, "validatorpersister", t.TempDir(),
				&configtempl.QueryServiceOrVCServiceConfig{
					CommonEndpoints: configtempl.CommonEndpoints{
						ServerEndpoint:  "localhost:6001",
						MetricsEndpoint: "localhost:6002",
					},
					DatabaseEndpoints: []*connection.Endpoint{
						connection.CreateEndpoint("host1:1111"),
						connection.CreateEndpoint("host2:2222"),
					},
					DatabaseName: "yugabyte",
				}),
			expectedConfig: &vcservice.ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6001,
					},
					Creds: nil,
				},
				Database: &vcservice.DatabaseConfig{
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
				ResourceLimits: &vcservice.ResourceLimitsConfig{
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
			expectedConfig: &vcservice.ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6001,
					},
				},
				Database: &vcservice.DatabaseConfig{
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
				ResourceLimits: &vcservice.ResourceLimitsConfig{
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
				require.NoError(t, config.ReadYamlConfigs([]string{tt.configFilePath}))
			}

			c := readConfig()

			require.Equal(t, tt.expectedConfig, c)

			dataSourceName := c.Database.DataSourceName()
			require.Equal(t, tt.expectedDataSourceName, dataSourceName)
		})
	}
}
