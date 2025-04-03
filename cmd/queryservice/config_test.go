package main

import (
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/query"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/vc"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

//nolint:paralleltest // Cannot parallelize due to viper.
func TestConfig(t *testing.T) {
	tests := []struct {
		name                   string
		configFilePath         string
		expectedConfig         *query.Config
		expectedDataSourceName string
	}{
		{
			name:           "valid config",
			configFilePath: "../config/samples/config-queryservice.yaml",
			expectedConfig: &query.Config{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 7003,
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
				},
				Monitoring: monitoring.Config{
					Server: &connection.ServerConfig{
						Endpoint: connection.Endpoint{
							Host: "localhost",
							Port: 7004,
						},
					},
				},
				MinBatchKeys:          1024,
				MaxBatchWait:          100 * time.Millisecond,
				ViewAggregationWindow: 100 * time.Millisecond,
				MaxAggregatedViews:    1024,
				MaxViewTimeout:        10 * time.Second,
			},
			expectedDataSourceName: "postgres://yugabyte:yugabyte@localhost:5433/yugabyte?sslmode=disable",
		},
		{
			name:           "no config file",
			configFilePath: "",
			expectedConfig: &query.Config{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 7003,
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
				},
				Monitoring: monitoring.Config{
					Server: &connection.ServerConfig{
						Endpoint: connection.Endpoint{
							Host: "localhost",
							Port: 7004,
						},
					},
				},
				MinBatchKeys:          1024,
				MaxBatchWait:          100 * time.Millisecond,
				ViewAggregationWindow: 100 * time.Millisecond,
				MaxAggregatedViews:    1024,
				MaxViewTimeout:        10 * time.Second,
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
