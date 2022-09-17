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
}
type ShardsServerMgrConfig struct {
	ShardsServersToNumShards map[string]int
	CleanupShards            bool
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
	}

	shardsServers := viper.GetStringSlice("shards_service.servers")
	numShardsPerServer := viper.GetInt("shards_service.num_shards_per_server")

	shardsServersToNumShards := map[string]int{}
	for _, s := range shardsServers {
		shardsServersToNumShards[s] = numShardsPerServer
	}
	shardsServerMgrConfig := &ShardsServerMgrConfig{
		ShardsServersToNumShards: shardsServersToNumShards,
		CleanupShards:            viper.GetBool("shards_service.delete_existing_shards"),
	}

	return &Config{
		SigVerifierMgrConfig:  sigVerifierMgrConfig,
		ShardsServerMgrConfig: shardsServerMgrConfig,
	}
}
