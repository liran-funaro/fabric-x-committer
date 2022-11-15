package pipeline

import (
	"sort"
	"time"

	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

type SigVerifierMgrConfig struct {
	Endpoints []*connection.Endpoint `mapstructure:"endpoints"`
}

type ShardsServerMgrConfig struct {
	Servers                       []*ShardServerInstanceConfig `mapstructure:"servers"`
	DeleteExistingShards          bool                         `mapstructure:"delete-existing-shards"`
	PrefixSizeForShardCalculation int                          `mapstructure:"prefix-size-for-shard-calculation"`
}

func (c *ShardsServerMgrConfig) GetEndpoints() []*connection.Endpoint {
	result := make([]*connection.Endpoint, len(c.Servers))
	for i, server := range c.Servers {
		result[i] = server.Endpoint
	}
	return result
}

func (c *ShardsServerMgrConfig) SortServers() {
	sort.SliceStable(c.Servers, func(i int, j int) bool {
		return c.Servers[i].Endpoint.Address() < c.Servers[j].Endpoint.Address()
	})
}

type ShardServerInstanceConfig struct {
	Endpoint  *connection.Endpoint `mapstructure:"endpoint"`
	NumShards int                  `mapstructure:"num-shards"`
}

type CoordinatorConfig struct {
	Prometheus    monitoring.Prometheus  `mapstructure:"prometheus"`
	Endpoint      connection.Endpoint    `mapstructure:"endpoint"`
	SigVerifiers  *SigVerifierMgrConfig  `mapstructure:"sig-verifiers"`
	ShardsServers *ShardsServerMgrConfig `mapstructure:"shards-servers"`
	Limits        *LimitsConfig          `mapstructure:"limits"`
}

type LimitsConfig struct {
	ShardRequestCutTimeout       time.Duration `mapstructure:"shard-request-cut-timeout"`
	MaxDependencyGraphSize       int           `mapstructure:"max-dependency-graph-size"`
	DependencyGraphUpdateTimeout time.Duration `mapstructure:"dependency-graph-update-timeout"`
	InvalidSigBatchCutoffSize    int           `mapstructure:"invalid-sig-batch-cutoff-size"`
}

func ReadConfig() CoordinatorConfig {
	wrapper := new(struct {
		Config CoordinatorConfig `mapstructure:"coordinator"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("coordinator.sig-verifiers.endpoints", []string{
		"localhost:5000",
	})

	viper.SetDefault("coordinator.shards-servers.delete-existing-shards", true)
	viper.SetDefault("coordinator.shards-servers.prefix-size-for-shard-calculation", 2)
	viper.SetDefault("coordinator.shards-servers.servers", []map[string]interface{}{
		{"endpoint": "localhost:5001", "num-shards": 4},
	})

	viper.SetDefault("coordinator.endpoint", ":5002")
	viper.SetDefault("coordinator.prometheus.endpoint", ":2112")
	viper.SetDefault("coordinator.prometheus.latency-endpoint", ":14268")

	viper.SetDefault("coordinator.limits.shard-request-cut-timeout", 1*time.Millisecond)
	viper.SetDefault("coordinator.limits.max-dependency-graph-size", 1000000) // 32 bytes per serial number, would cause roughly 32MB memory
	viper.SetDefault("coordinator.limits.dependency-graph-update-timeout", 1*time.Millisecond)
	viper.SetDefault("coordinator.limits.invalid-sig-batch-cutoff-size", 1*time.Millisecond)
}
