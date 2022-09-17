package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

func TestSigVerifiersMgr(t *testing.T) {
	sigVerifierServer, err := testutil.NewSigVerifierGrpcServer(
		testutil.DefaultSigVerifierBehavior,
		config.DefaultGRPCPortSigVerifier,
	)
	require.NoError(t, err)
	defer sigVerifierServer.Stop()

	m, err := newSigVerificationMgr(
		&SigVerifierMgrConfig{
			SigVerifierServers: []string{"localhost"},
		},
	)
	assert.NoError(t, err)
	defer m.stop()

	m.inputChan <- &token.Block{
		Number: uint64(0),
		Txs: []*token.Tx{
			{SerialNumbers: [][]byte{[]byte("00"), []byte("01")}},
			{SerialNumbers: [][]byte{[]byte("12"), []byte("13")}},
		},
	}

	txSeqNums := <-m.outputChanValids
	require.ElementsMatch(t,
		txSeqNums,
		[]TxSeqNum{
			{BlkNum: 0, TxNum: 0},
			{BlkNum: 0, TxNum: 1},
		},
	)
}
