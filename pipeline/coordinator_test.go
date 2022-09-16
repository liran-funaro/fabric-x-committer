package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

func TestCoordinator(t *testing.T) {
	sigVerifierServer, err := testutil.NewSigVerifierGrpcServer(
		testutil.DefaultSigVerifierBehavior,
		config.DefaultGRPCPortSigVerifier,
	)
	require.NoError(t, err)
	defer sigVerifierServer.Stop()

	shardsServer, err := testutil.NewShardsServer(
		testutil.DefaultPhaseOneBehavior,
		config.DefaultGRPCPortShardsServer,
	)
	require.NoError(t, err)
	defer shardsServer.Stop()

	c := &pipeline.Config{
		SigVerifierMgrConfig: &pipeline.SigVerifierMgrConfig{
			SigVerifierServers: []string{"localhost"},
			BatchCutConfig: &pipeline.BatchConfig{
				BatchSize:     2,
				TimeoutMillis: 20000,
			},
		},

		ShardsServerMgrConfig: &pipeline.ShardsServerMgrConfig{
			ShardsServersToNumShards: map[string]int{"localhost": 1},
			BatchConfig: &pipeline.BatchConfig{
				BatchSize:     2,
				TimeoutMillis: 20000,
			},
		},
	}

	coordinator, err := pipeline.NewCoordinator(c)
	require.NoError(t, err)
	defer coordinator.Stop()

	block := &token.Block{
		Number: 0,
		Txs: []*token.Tx{
			{SerialNumbers: [][]byte{[]byte("00"), []byte("01")}},
			{SerialNumbers: [][]byte{[]byte("12"), []byte("13")}},
		},
	}
	coordinator.ProcessBlockAsync(block)

	status := <-coordinator.TxStatusChan()
	require.ElementsMatch(t,
		status,
		[]*pipeline.TxStatus{
			{
				TxSeqNum: pipeline.TxSeqNum{
					BlkNum: 0,
					TxNum:  0,
				},
				IsValid: true,
			},
			{
				TxSeqNum: pipeline.TxSeqNum{
					BlkNum: 0,
					TxNum:  1,
				},
				IsValid: true,
			},
		},
	)
}
