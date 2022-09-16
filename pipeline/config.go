package pipeline

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	SigVerifierMgrConfig  *SigVerifierMgrConfig
	ShardsServerMgrConfig *ShardsServerMgrConfig
}

type SigVerifierMgrConfig struct {
	SigVerifierServers []string
	BatchCutConfig     *BatchConfig
}

type BatchConfig struct {
	BatchSize     int
	TimeoutMillis int
}

type ShardsServerMgrConfig struct {
	ShardsServersToNumShards map[string]int
	BatchConfig              *BatchConfig
	CleanupShards            bool
}

var DefaultBatchConfig = &BatchConfig{
	BatchSize:     10000,
	TimeoutMillis: 10,
}

func LoadConfigFromYaml(path string) *Config {
	viper.AddConfigPath(path)
	viper.SetConfigName("config-coordinator")
	viper.SetConfigType("yaml")

	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Sprintf("Error in loading config file: %s", err))
	}

	sigVerifierMgrConfig := &SigVerifierMgrConfig{
		SigVerifierServers: viper.GetStringSlice("sig_verification.servers"),
		BatchCutConfig:     DefaultBatchConfig,
	}

	shardsServers := viper.GetStringSlice("shards_service.servers")
	numShardsPerServer := viper.GetInt("shards_service.num_shards_per_server")

	shardsServersToNumShards := map[string]int{}
	for _, s := range shardsServers {
		shardsServersToNumShards[s] = numShardsPerServer
	}
	shardsServerMgrConfig := &ShardsServerMgrConfig{
		ShardsServersToNumShards: shardsServersToNumShards,
		BatchConfig:              DefaultBatchConfig,
		CleanupShards:            viper.GetBool("shards_service.delete_existing_shards"),
	}

	return &Config{
		SigVerifierMgrConfig:  sigVerifierMgrConfig,
		ShardsServerMgrConfig: shardsServerMgrConfig,
	}
}
