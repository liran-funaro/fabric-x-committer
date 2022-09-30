package pipeline_test

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

var conf = &pipeline.CoordinatorConfig{
	SigVerifiers: &pipeline.SigVerifierMgrConfig{
		Endpoints: []*connection.Endpoint{connection.CreateEndpoint("localhost:5000")},
	},
	ShardsServers: &pipeline.ShardsServerMgrConfig{
		Servers:              []*pipeline.ShardServerInstanceConfig{{connection.CreateEndpoint("localhost:5001"), 1}},
		DeleteExistingShards: true,
	},
}

func TestCoordinator(t *testing.T) {
	sigVerifierServer, err := testutil.NewSigVerifierGrpcServer(
		testutil.DefaultSigVerifierBehavior,
		conf.SigVerifiers.Endpoints[0],
	)
	require.NoError(t, err)
	defer sigVerifierServer.Stop()

	shardsServer, err := testutil.NewShardsGrpcServer(
		testutil.DefaultPhaseOneBehavior,
		conf.ShardsServers.Servers[0].Endpoint,
	)
	require.NoError(t, err)
	defer shardsServer.Stop()

	coordinator, err := pipeline.NewCoordinator(conf.SigVerifiers, conf.ShardsServers, false)
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
