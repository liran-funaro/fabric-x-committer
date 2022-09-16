package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
)

func TestLoadConfig(t *testing.T) {
	c := pipeline.LoadConfigFromYaml("testdata")
	require.Equal(t,
		&pipeline.Config{
			SigVerifierMgrConfig: &pipeline.SigVerifierMgrConfig{
				SigVerifierServers: []string{"machine1", "machine2"},
				BatchCutConfig:     pipeline.DefaultBatchConfig,
			},

			ShardsServerMgrConfig: &pipeline.ShardsServerMgrConfig{
				ShardsServersToNumShards: map[string]int{
					"machine3": 4,
					"machine4": 4,
				},
				BatchConfig:   pipeline.DefaultBatchConfig,
				CleanupShards: true,
			},
		},
		c,
	)
}
