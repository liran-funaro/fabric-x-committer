package pipeline

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
)

func TestShardsServerMgr(t *testing.T) {
	shardsServer, err := testutil.NewShardsServer(
		testutil.DefaultPhaseOneBehavior,
		config.DefaultGRPCPortShardsServer,
	)
	require.NoError(t, err)
	defer shardsServer.Stop()

	m, err := newShardsServerMgr(
		&ShardsServerMgrConfig{
			BatchConfig: &BatchConfig{
				BatchSize:     2,
				TimeoutMillis: 2000,
			},
			CleanupShards: true,
			ShardsServersToNumShards: map[string]int{
				"localhost": 1,
			},
		},
	)
	require.NoError(t, err)
	defer m.stop()

	m.inputChan <- map[TxSeqNum][][]byte{
		{BlkNum: 1, TxNum: 1}: {[]byte("sn1"), []byte("sn2")},
		{BlkNum: 1, TxNum: 2}: {[]byte("sn3"), []byte("sn4")},
	}
	status := <-m.outputChan
	require.Len(t, status, 2)
	require.Contains(t,
		status,
		&TxStatus{
			TxSeqNum: TxSeqNum{
				BlkNum: 1,
				TxNum:  1,
			},
			IsValid: true,
		},
	)
	require.Contains(t,
		status,
		&TxStatus{
			TxSeqNum: TxSeqNum{
				BlkNum: 1,
				TxNum:  2,
			},
			IsValid: true,
		},
	)
}
