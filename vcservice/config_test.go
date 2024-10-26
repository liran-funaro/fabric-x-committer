package vcservice

import (
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
)

func TestConfig(t *testing.T) {
	tests := []struct {
		name                   string
		configFilePath         string
		expectedConfig         *ValidatorCommitterServiceConfig
		expectedDataSourceName string
	}{
		{
			name:           "valid config",
			configFilePath: "../config/samples/config-vcservice.yaml",
			expectedConfig: &ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6002,
					},
				},
				Database: &DatabaseConfig{
					Host:                  "localhost",
					Port:                  5433,
					Username:              "yugabyte",
					Password:              "yugabyte",
					Database:              "yugabyte",
					MaxConnections:        10,
					MinConnections:        5,
					ConnPoolCreateTimeout: 15 * time.Second,
				},
				ResourceLimits: &ResourceLimitsConfig{
					MaxWorkersForPreparer:             1,
					MaxWorkersForValidator:            1,
					MaxWorkersForCommitter:            20,
					MinTransactionBatchSize:           1,
					TimeoutForMinTransactionBatchSize: 2 * time.Second,
				},
				Monitoring: &monitoring.Config{
					Metrics: &metrics.Config{
						Enable: true,
						Endpoint: &connection.Endpoint{
							Host: "",
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
			expectedConfig: &ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6001,
					},
					Creds: nil,
				},
				Database: &DatabaseConfig{
					Host:                  "localhost",
					Port:                  5433,
					Username:              "yugabyte",
					Password:              "yugabyte",
					Database:              "yugabyte",
					MaxConnections:        20,
					MinConnections:        10,
					ConnPoolCreateTimeout: 20 * time.Second,
				},
				ResourceLimits: &ResourceLimitsConfig{
					MaxWorkersForPreparer:             1,
					MaxWorkersForValidator:            1,
					MaxWorkersForCommitter:            20,
					MinTransactionBatchSize:           1,
					TimeoutForMinTransactionBatchSize: 5 * time.Second,
				},
				Monitoring: &monitoring.Config{
					Metrics: &metrics.Config{
						Enable: true,
						Endpoint: &connection.Endpoint{
							Host: "localhost",
							Port: 6002,
						},
					},
				},
			},
			expectedDataSourceName: "postgres://yugabyte:yugabyte@localhost:5433/yugabyte?sslmode=disable",
		},
		{
			name:           "no config file",
			configFilePath: "",
			expectedConfig: &ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6001,
					},
				},
				Database: &DatabaseConfig{
					Host:                  "localhost",
					Port:                  5433,
					Username:              "yugabyte",
					Password:              "yugabyte",
					Database:              "yugabyte",
					MaxConnections:        20,
					MinConnections:        10,
					ConnPoolCreateTimeout: 20 * time.Second,
				},
				ResourceLimits: &ResourceLimitsConfig{
					MaxWorkersForPreparer:             1,
					MaxWorkersForValidator:            1,
					MaxWorkersForCommitter:            20,
					MinTransactionBatchSize:           1,
					TimeoutForMinTransactionBatchSize: 5 * time.Second,
				},
				Monitoring: &monitoring.Config{
					Metrics: &metrics.Config{
						Enable: true,
						Endpoint: &connection.Endpoint{
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

			c := ReadConfig()

			require.Equal(t, tt.expectedConfig, c)

			dataSourceName := c.Database.DataSourceName()
			require.Equal(t, tt.expectedDataSourceName, dataSourceName)
		})
	}
}
