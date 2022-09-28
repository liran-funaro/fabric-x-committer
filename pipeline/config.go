package pipeline

import (
	"sort"

	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type SigVerifierMgrConfig struct {
	Servers []*connection.Endpoint `mapstructure:"servers"`
}

type ShardsServerMgrConfig struct {
	Servers              []*ShardServerInstanceConfig `mapstructure:"servers"`
	DeleteExistingShards bool                         `mapstructure:"delete-existing-shards"`
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
	Prometheus    connection.Prometheus  `mapstructure:"prometheus"`
	Endpoint      connection.Endpoint    `mapstructure:"endpoint"`
	SigVerifiers  *SigVerifierMgrConfig  `mapstructure:"sig-verifiers"`
	ShardsServers *ShardsServerMgrConfig `mapstructure:"shards-servers"`
}

func (c *CoordinatorConfig) Connection() *connection.ServerConfig {
	return &connection.ServerConfig{Prometheus: c.Prometheus, Endpoint: c.Endpoint}
}

var configWrapper struct {
	Config CoordinatorConfig `mapstructure:"coordinator"`
}

var Config = &configWrapper.Config

func init() {
	viper.SetDefault("coordinator.sig-verifiers.servers", []string{
		"localhost:5000",
	})

	viper.SetDefault("coordinator.shards-servers.delete-existing-shards", true)
	viper.SetDefault("coordinator.shards-servers.servers", []map[string]interface{}{
		{"endpoint": "localhost:5001", "num-shards": 4},
	})

	viper.SetDefault("coordinator.endpoint", "localhost:5002")
	viper.SetDefault("coordinator.prometheus.enabled", "false")
	viper.SetDefault("coordinator.prometheus.endpoint", "localhost:2114")

	config.Unmarshal(&configWrapper)
}
