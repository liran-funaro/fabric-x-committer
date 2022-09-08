package config

const DefaultGRPCPortSigVerifier = 5000
const DefaultGRPCPortShardsServer = 5001

type Config struct {
	SigVerifierMgrConfig *SigVerifierMgrConfig
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
