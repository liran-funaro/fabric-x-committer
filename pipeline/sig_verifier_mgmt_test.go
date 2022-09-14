package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
)

func TestSigVerifiersMgr(t *testing.T) {
	s := testutil.NewSigVerifierGrpcServer(
		t, testutil.DefaultSigVerifierBehavior, config.DefaultGRPCPortSigVerifier,
	)
	defer s.Stop()

	m, err := newSigVerificationMgr(
		&SigVerifierMgrConfig{
			SigVerifierServers: []string{"localhost"},
			BatchCutConfig: &BatchConfig{
				BatchSize:     2,
				TimeoutMillis: 20000,
			},
		},
	)
	assert.NoError(t, err)
	defer m.stop()

	g := testutil.NewBlockGenerator(2, 1)
	defer g.Stop()

	m.inputChan <- <-g.OutputChan()
	txSeqNums := <-m.outputChanValids

	require.Equal(t, 2, len(txSeqNums))
	require.Equal(t, TxSeqNum{BlkNum: 0, TxNum: 0}, txSeqNums[0])
	require.Equal(t, TxSeqNum{BlkNum: 0, TxNum: 1}, txSeqNums[1])
}
