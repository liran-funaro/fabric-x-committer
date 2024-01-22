package queryservice

import (
	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

// Config is the configuration for the query service.
type Config struct {
	Server     *connection.ServerConfig  `mapstructure:"server"`
	Database   *vcservice.DatabaseConfig `mapstructure:"database"`
	Monitoring *monitoring.Config        `mapstructure:"monitoring"`
}

// ReadConfig reads the configuration from the viper instance.
// If the configuration file is used, the caller should call
// config.ReadFromYamlFile() before calling this function.
func ReadConfig() *Config {
	setDefaults()

	wrapper := new(struct {
		Config Config `mapstructure:"query-service"`
	})
	config.Unmarshal(wrapper)
	return &wrapper.Config
}

func setDefaults() {
	// defaults for ServerConfig
	viper.SetDefault("query-service.server.endpoint", "localhost:7003")

	// defaults for DatabaseConfig
	prefix := "query-service.database."
	viper.SetDefault(prefix+"host", "localhost")
	viper.SetDefault(prefix+"port", 5433)
	viper.SetDefault(prefix+"username", "yugabyte")
	viper.SetDefault(prefix+"password", "yugabyte")
	viper.SetDefault(prefix+"database", "yugabyte")
	viper.SetDefault(prefix+"max-connections", 20)
	viper.SetDefault(prefix+"min-connections", 10)

	// defaults for monitoring.config
	prefix = "query-service.monitoring."
	viper.SetDefault(prefix+"metrics.endpoint", "localhost:7004")
	viper.SetDefault(prefix+"metrics.enable", true)
}
