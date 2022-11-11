package shardsservice

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"time"

	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type ShardDbType = string

const (
	RocksDb   ShardDbType = "rocksdb"
	GoLevelDb             = "goleveldb"
	MockDb                = "mockdb"
)

type DatabaseConfig struct {
	Type    ShardDbType `mapstructure:"type"`
	RootDir string      `mapstructure:"root-dir"`
}

type LimitsConfig struct {
	MaxPhaseOneResponseBatchItemCount uint32        `mapstructure:"max-phase-one-response-batch-item-count"`
	MaxShardInstancesBufferSize       uint32        `mapstructure:"max-shard-instances-buffer-size"`
	MaxPendingCommitsBufferSize       uint32        `mapstructure:"max-pending-commits-buffer-size"`
	MaxPhaseOneProcessingWorkers      uint32        `mapstructure:"max-phase-one-processing-workers"`
	MaxPhaseTwoProcessingWorkers      uint32        `mapstructure:"max-phase-two-processing-workers"`
	PhaseOneResponseCutTimeout        time.Duration `mapstructure:"phase-one-response-cut-timeout"`
}

type ShardServiceConfig struct {
	Prometheus monitoring.Prometheus `mapstructure:"prometheus"`
	Endpoint   connection.Endpoint   `mapstructure:"endpoint"`
	Database   *DatabaseConfig       `mapstructure:"database"`
	Limits     *LimitsConfig         `mapstructure:"limits"`
}

func ReadConfig() ShardServiceConfig {
	wrapper := new(struct {
		Config ShardServiceConfig `mapstructure:"shards-service"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("shards-service.database.type", "rocksdb")
	viper.SetDefault("shards-service.database.root-dir", "./")

	viper.SetDefault("shards-service.limits.max-phase-one-response-batch-item-count", 100)
	viper.SetDefault("shards-service.limits.phase-one-response-cut-timeout", 50*time.Millisecond)
	viper.SetDefault("shards-service.limits.max-shard-instances-buffer-size", 1_000_000)
	viper.SetDefault("shards-service.limits.max-pending-commits-buffer-size", 1_000_000)
	viper.SetDefault("shards-service.limits.max-phase-one-processing-workers", 50)
	viper.SetDefault("shards-service.limits.max-phase-two-processing-workers", 50)

	viper.SetDefault("shards-service.endpoint", "localhost:5001")
	viper.SetDefault("shards-service.prometheus.enabled", true)
	viper.SetDefault("shards-service.prometheus.latency-enabled", true)
	viper.SetDefault("shards-service.prometheus.endpoint", "localhost:2112")
}
