package shardsservice

import (
	"time"

	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type ShardCoordinatorConfig struct {
	Database *DatabaseConfig
	Limits   *LimitsConfig
}

type DatabaseConfig struct {
	Name    string `mapstructure:"name"`
	RootDir string `mapstructure:"root-dir"`
}

type LimitsConfig struct {
	MaxGoroutines                     uint32        `mapstructure:"max-goroutines"`
	MaxPhaseOneResponseBatchItemCount uint32        `mapstructure:"max-phase-one-response-batch-item-count"`
	PhaseOneResponseCutTimeout        time.Duration `mapstructure:"phase-one-response-cut-timeout"`
}

type ShardServiceConfig struct {
	Prometheus connection.Prometheus `mapstructure:"prometheus"`
	Endpoint   connection.Endpoint   `mapstructure:"endpoint"`
	Database   DatabaseConfig        `mapstructure:"database"`
	Limits     LimitsConfig          `mapstructure:"limits"`
}

func (c *ShardServiceConfig) ShardCoordinator() *ShardCoordinatorConfig {
	return &ShardCoordinatorConfig{
		Database: &c.Database,
		Limits:   &c.Limits,
	}
}

func (c *ShardServiceConfig) Connection() *connection.ServerConfig {
	return &connection.ServerConfig{Prometheus: c.Prometheus, Endpoint: c.Endpoint}
}

var configWrapper struct {
	Config ShardServiceConfig `mapstructure:"shards-service"`
}

var Config = &configWrapper.Config

func init() {
	viper.SetDefault("shards-service.database.name", "rocksdb")
	viper.SetDefault("shards-service.database.root-dir", "./")

	viper.SetDefault("shards-service.limits.max-goroutines", 100)
	viper.SetDefault("shards-service.limits.max-phase-one-response-batch-item-count", 100)
	viper.SetDefault("shards-service.limits.phase-one-response-cut-timeout", 50*time.Millisecond)

	viper.SetDefault("shards-service.endpoint", "localhost:5001")
	viper.SetDefault("shards-service.prometheus.enabled", "true")
	viper.SetDefault("shards-service.prometheus.endpoint", "localhost:2113")

	config.Unmarshal(&configWrapper)
}
