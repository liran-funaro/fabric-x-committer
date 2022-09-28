package pipeline

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

func TestSigVerifiersMgr(t *testing.T) {
	// setup
	verifierServers := []*connection.Endpoint{
		connection.CreateEndpoint("localhost:5000"),
		connection.CreateEndpoint("localhost:5001"),
		connection.CreateEndpoint("localhost:5002"),
	}
	sigVerifierServer, err := testutil.StartsSigVerifierGrpcServers(
		testutil.DefaultSigVerifierBehavior,
		verifierServers,
	)
	require.NoError(t, err)
	defer func() {
		for _, s := range sigVerifierServer {
			s.Stop()
		}
	}()

	m, err := newSigVerificationMgr(
		&SigVerifierMgrConfig{
			Servers: verifierServers,
		},
	)
	assert.NoError(t, err)
	defer m.stop()

	bg := testutil.NewBlockGenerator(2, 1, false)
	defer bg.Stop()

	wg := sync.WaitGroup{}
	wg.Add(2)

	// send blocks to mgr
	go func() {
		for i := 0; i < 4; i++ {
			m.inputChan <- <-bg.OutputChan()
		}
		wg.Done()
	}()

	txSeqNums := []TxSeqNum{}
	go func() {
		for i := 0; i < 4; i++ {
			txSeqNums = append(txSeqNums, <-m.outputChanValids...)
		}
		wg.Done()
	}()

	wg.Wait()
	require.ElementsMatch(t,
		txSeqNums,
		[]TxSeqNum{
			{BlkNum: uint64(0), TxNum: 0},
			{BlkNum: uint64(0), TxNum: 1},
			{BlkNum: uint64(1), TxNum: 0},
			{BlkNum: uint64(1), TxNum: 1},
			{BlkNum: uint64(2), TxNum: 0},
			{BlkNum: uint64(2), TxNum: 1},
			{BlkNum: uint64(3), TxNum: 0},
			{BlkNum: uint64(3), TxNum: 1},
		},
	)
	require.Equal(t, 2, sigVerifierServer[0].SigVerifierImpl.Stats.NumBatchesServed)
	require.Equal(t, 1, sigVerifierServer[1].SigVerifierImpl.Stats.NumBatchesServed)
	require.Equal(t, 1, sigVerifierServer[2].SigVerifierImpl.Stats.NumBatchesServed)
}
