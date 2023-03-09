package pipeline

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
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

	mgr, err := newSigVerificationMgr(
		&SigVerifierMgrConfig{
			Endpoints: verifierServers,
		},
		(&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics),
	)
	assert.NoError(t, err)
	defer mgr.stop()

	bg := testutil.NewBlockGenerator(2, 1, false)
	defer bg.Stop()

	wg := sync.WaitGroup{}
	wg.Add(2)

	// send blocks to mgr
	go func() {
		for i := 0; i < 4; i++ {
			mgr.inputChan <- <-bg.OutputChan()
		}
		wg.Done()
	}()

	txSeqNums := []TxSeqNum{}
	go func() {
		for i := 0; i < 4; i++ {
			txSeqNums = append(txSeqNums, <-mgr.outputChanValids...)
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
	require.Equal(t, 4, sigVerifierServer[0].SigVerifierImpl.Stats.NumBatchesServed+sigVerifierServer[1].SigVerifierImpl.Stats.NumBatchesServed+sigVerifierServer[2].SigVerifierImpl.Stats.NumBatchesServed)
}
