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
			configFilePath: "./testdata/valid_config.yaml",
			expectedConfig: &ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6002,
					},
					Creds: nil,
					KeepAlive: &connection.ServerKeepAliveConfig{
						Params: &connection.ServerKeepAliveParamsConfig{
							MaxConnectionIdle:     5 * time.Second,
							MaxConnectionAge:      10 * time.Second,
							MaxConnectionAgeGrace: 2 * time.Second,
							Time:                  5 * time.Second,
							Timeout:               1 * time.Second,
						},
						EnforcementPolicy: &connection.ServerKeepAliveEnforcementPolicyConfig{
							MinTime:             5 * time.Second,
							PermitWithoutStream: true,
						},
					},
				},
				Database: &DatabaseConfig{
					Host:           "localhost",
					Port:           5433,
					Username:       "yugabyte",
					Password:       "yugabyte",
					Database:       "yugabyte",
					MaxConnections: 10,
					MinConnections: 5,
				},
				ResourceLimits: &ResourceLimitsConfig{
					MaxWorkersForPreparer:  2,
					MaxWorkersForValidator: 2,
					MaxWorkersForCommitter: 2,
				},
				Monitoring: &monitoring.Config{
					Metrics: &metrics.Config{
						Endpoint: &connection.Endpoint{
							Host: "",
							Port: 2111,
						},
					},
				},
			},
			expectedDataSourceName: "host=localhost port=5433 user=yugabyte password=yugabyte sslmode=disable",
		},
		{
			name:           "defualt config",
			configFilePath: "testdata/default_config.yaml",
			expectedConfig: &ValidatorCommitterServiceConfig{
				Server: &connection.ServerConfig{
					Endpoint: connection.Endpoint{
						Host: "localhost",
						Port: 6001,
					},
					Creds: nil,
					KeepAlive: &connection.ServerKeepAliveConfig{
						Params: &connection.ServerKeepAliveParamsConfig{
							MaxConnectionIdle:     5 * time.Second,
							MaxConnectionAge:      30 * time.Second,
							MaxConnectionAgeGrace: 5 * time.Second,
							Time:                  5 * time.Second,
							Timeout:               1 * time.Second,
						},
						EnforcementPolicy: &connection.ServerKeepAliveEnforcementPolicyConfig{
							MinTime:             1 * time.Second,
							PermitWithoutStream: true,
						},
					},
				},
				Database: &DatabaseConfig{
					Host:           "localhost",
					Port:           5433,
					Username:       "yugabyte",
					Password:       "yugabyte",
					Database:       "yugabyte",
					MaxConnections: 20,
					MinConnections: 10,
				},
				ResourceLimits: &ResourceLimitsConfig{
					MaxWorkersForPreparer:  10,
					MaxWorkersForValidator: 10,
					MaxWorkersForCommitter: 10,
				},
				Monitoring: &monitoring.Config{
					Metrics: &metrics.Config{
						Endpoint: &connection.Endpoint{
							Host: "localhost",
							Port: 6002,
						},
					},
				},
			},
			expectedDataSourceName: "host=localhost port=5433 user=yugabyte password=yugabyte sslmode=disable",
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
					Creds: nil,
					KeepAlive: &connection.ServerKeepAliveConfig{
						Params: &connection.ServerKeepAliveParamsConfig{
							MaxConnectionIdle:     5 * time.Second,
							MaxConnectionAge:      30 * time.Second,
							MaxConnectionAgeGrace: 5 * time.Second,
							Time:                  5 * time.Second,
							Timeout:               1 * time.Second,
						},
						EnforcementPolicy: &connection.ServerKeepAliveEnforcementPolicyConfig{
							MinTime:             1 * time.Second,
							PermitWithoutStream: true,
						},
					},
				},
				Database: &DatabaseConfig{
					Host:           "localhost",
					Port:           5433,
					Username:       "yugabyte",
					Password:       "yugabyte",
					Database:       "yugabyte",
					MaxConnections: 20,
					MinConnections: 10,
				},
				ResourceLimits: &ResourceLimitsConfig{
					MaxWorkersForPreparer:  10,
					MaxWorkersForValidator: 10,
					MaxWorkersForCommitter: 10,
				},
				Monitoring: &monitoring.Config{
					Metrics: &metrics.Config{
						Endpoint: &connection.Endpoint{
							Host: "localhost",
							Port: 6002,
						},
					},
				},
			},
			expectedDataSourceName: "host=localhost port=5433 user=yugabyte password=yugabyte sslmode=disable",
		},
	}

	for _, test := range tests {
		tt := test
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			require.NoError(t, config.ReadYamlConfigs([]string{tt.configFilePath}))
			c := ReadConfig()

			require.Equal(t, tt.expectedConfig, c)

			dataSourceName := c.Database.DataSourceName()
			require.Equal(t, tt.expectedDataSourceName, dataSourceName)
		})
	}
}
