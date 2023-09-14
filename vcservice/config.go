package vcservice

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

var defaultLocalConfigFile = "config.yaml"

// ValidatorCommitterServiceConfig is the configuration for the validator-committer service.
type ValidatorCommitterServiceConfig struct {
	Server         *connection.ServerConfig `mapstructure:"server"`
	Database       *DatabaseConfig          `mapstructure:"database"`
	ResourceLimits *ResourceLimitsConfig    `mapstructure:"resource-limits"`
	Monitoring     *monitoring.Config       `mapstructure:"monitoring"`
}

// DatabaseConfig is the configuration for the database.
type DatabaseConfig struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	Username       string `mapstructure:"username"`
	Password       string `mapstructure:"password"`
	Database       string `mapstructure:"database"`
	MaxConnections int32  `mapstructure:"max-connections"`
	MinConnections int32  `mapstructure:"min-connections"`
	LoadBalance    bool   `mapstructure:"load-balance"`
}

// DataSourceName returns the data source name of the database.
func (d *DatabaseConfig) DataSourceName() string {
	ret := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		d.Username, d.Password, d.Host, d.Port, d.Database)

	// The load balancing flag is only available when the server supports it (having multiple nodes).
	// Thus, we only add it when explicitly required. Otherwise, an error will occur.
	if d.LoadBalance {
		ret += "&load_balance=true"
	}
	return ret
}

// ResourceLimitsConfig is the configuration for the resource limits.
type ResourceLimitsConfig struct {
	MaxWorkersForPreparer  int `mapstructure:"max-workers-for-preparer"`
	MaxWorkersForValidator int `mapstructure:"max-workers-for-validator"`
	MaxWorkersForCommitter int `mapstructure:"max-workers-for-committer"`
}

// ReadConfig reads the configuration from the viper instance.
// If the configuration file is used, the caller should call
// config.ReadFromYamlFile() before calling this function.
func ReadConfig() *ValidatorCommitterServiceConfig {
	setDefaults()

	wrapper := new(struct {
		Config ValidatorCommitterServiceConfig `mapstructure:"validator-committer-service"`
	})
	config.Unmarshal(wrapper)
	return &wrapper.Config
}

func setDefaults() {
	// defaults for ServerConfig
	prefix := "validator-committer-service.server.endpoint."
	viper.SetDefault(prefix+"host", "localhost")
	viper.SetDefault(prefix+"port", 6001)

	// default for ServerConfig.keepAlive
	prefix = "validator-committer-service.server.keep-alive.params."
	viper.SetDefault(prefix+"max-connection-idle", 5*time.Second)
	viper.SetDefault(prefix+"time", 5*time.Second)
	viper.SetDefault(prefix+"timeout", 1*time.Second)
	// We don't want to limit the connection age since it is expected to live forever
	// TODO: remove this configuration values for the vcservice so the developer cannot set them accidentally.
	viper.SetDefault(prefix+"max-connection-age", 0)
	viper.SetDefault(prefix+"max-connection-age-grace", 0)

	// default for ServerConfig.keepAlive.enforcementPolicy
	prefix = "validator-committer-service.server.keep-alive.enforcement-policy."
	viper.SetDefault(prefix+"min-time", 1*time.Second)
	viper.SetDefault(prefix+"permit-without-stream", true)

	// defaults for DatabaseConfig
	prefix = "validator-committer-service.database."
	viper.SetDefault(prefix+"host", "localhost")
	viper.SetDefault(prefix+"port", 5433)
	viper.SetDefault(prefix+"username", "yugabyte")
	viper.SetDefault(prefix+"password", "yugabyte")
	viper.SetDefault(prefix+"database", "yugabyte")
	viper.SetDefault(prefix+"max-connections", 20)
	viper.SetDefault(prefix+"min-connections", 10)

	// defaults for ResourceLimitsConfig
	prefix = "validator-committer-service.resource-limits."
	viper.SetDefault(prefix+"max-workers-for-preparer", 10)
	viper.SetDefault(prefix+"max-workers-for-validator", 10)
	viper.SetDefault(prefix+"max-workers-for-committer", 10)

	// defaults for monitoring.config
	prefix = "validator-committer-service.monitoring."
	viper.SetDefault(prefix+"metrics.endpoint", "localhost:6002")
}
