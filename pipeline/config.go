package pipeline

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
