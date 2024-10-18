package coordinatorservice

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

type svMgrTestEnv struct {
	signVerifierManager       *signatureVerifierManager
	inputBlock                chan *protoblocktx.Block
	outputBlockWithValidTxs   chan *protoblocktx.Block
	outputBlockWithInvalidTxs chan *protoblocktx.Block
	mockSvService             []*sigverifiermock.MockSigVerifier
	grpcServers               []*grpc.Server
	serversConfig             []*connection.ServerConfig
}

func newSvMgrTestEnv(t *testing.T, numSvService int) *svMgrTestEnv {
	sc, svs, grpcServers := sigverifiermock.StartMockSVService(numSvService)
	t.Cleanup(func() {
		for _, s := range grpcServers {
			s.GracefulStop()
		}

		for _, mockSv := range svs {
			mockSv.Close()
		}
	})

	inputBlock := make(chan *protoblocktx.Block, 10)
	outputBlockWithValidTxs := make(chan *protoblocktx.Block, 10)
	outputBlockWithInvalidTxs := make(chan *protoblocktx.Block, 10)

	svm := newSignatureVerifierManager(
		&signVerifierManagerConfig{
			serversConfig:                         sc,
			incomingBlockForSignatureVerification: inputBlock,
			outgoingBlockWithValidTxs:             outputBlockWithValidTxs,
			outgoingBlockWithInvalidTxs:           outputBlockWithInvalidTxs,
			metrics:                               newPerformanceMetrics(true),
		},
	)

	wg := sync.WaitGroup{}
	t.Cleanup(wg.Wait)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	wg.Add(1)
	go func() { require.NoError(t, svm.run(ctx)); wg.Done() }()

	<-svm.connectionReady

	env := &svMgrTestEnv{
		signVerifierManager:       svm,
		inputBlock:                inputBlock,
		outputBlockWithValidTxs:   outputBlockWithValidTxs,
		outputBlockWithInvalidTxs: outputBlockWithInvalidTxs,
		mockSvService:             svs,
		grpcServers:               grpcServers,
		serversConfig:             sc,
	}

	return env
}

func requireBlockFromQueue(
	t *testing.T, expectedBlk *protoblocktx.Block, blkOutputChan chan *protoblocktx.Block,
) {
	select {
	case actualBlk := <-blkOutputChan:
		require.Equal(t, expectedBlk, actualBlk)
	case <-time.After(5 * time.Second):
		t.Fatal("Did not receive block from output after timeout")
	}
}

func (e *svMgrTestEnv) requireBlock(
	t *testing.T, expectedBlkWithValTxs, expectedBlkWithInvalidTxs *protoblocktx.Block,
) {
	requireBlockFromQueue(t, expectedBlkWithValTxs, e.outputBlockWithValidTxs)
	requireBlockFromQueue(t, expectedBlkWithInvalidTxs, e.outputBlockWithInvalidTxs)
}

func TestSignatureVerifierManagerWithSingleVerifier(t *testing.T) {
	// MockSigVerifier marks valid and invalid flag as follows:
	// - when the block number is even, the even numbered txs are valid and the odd numbered txs are invalid
	// - when the block number is odd, the even numbered txs are invalid and the odd numbered txs are valid
	env := newSvMgrTestEnv(t, 1)

	blkNum := 0
	numTxs := 10
	blk, expectedBlkWithValTxs, expectedBlkWithInvalTxs := createBlockForTest(t, blkNum, numTxs)
	env.inputBlock <- blk

	env.requireBlock(t, expectedBlkWithValTxs, expectedBlkWithInvalTxs)

	blkNum = 1
	numTxs = 1
	blk, expectedBlkWithValTxs, _ = createBlockForTest(t, blkNum, numTxs)
	env.inputBlock <- blk

	require.Equal(t, expectedBlkWithValTxs, <-env.outputBlockWithValidTxs)
	select {
	case b := <-env.outputBlockWithInvalidTxs:
		t.Fatal("should not have invalid txs", b)
	case <-time.After(500 * time.Millisecond):
	}

	blkNum = 2
	numTxs = 4
	blk, expectedBlkWithValTxs, expectedBlkWithInvalTxs = createBlockForTest(t, blkNum, numTxs)
	env.inputBlock <- blk

	env.requireBlock(t, expectedBlkWithValTxs, expectedBlkWithInvalTxs)

	require.Eventually(t, func() bool {
		return test.GetMetricValue(
			t,
			env.signVerifierManager.config.metrics.sigverifierTransactionProcessedTotal,
		) == 15
	}, 2*time.Second, 100*time.Millisecond)
}

func TestSignatureVerifierManagerWithMultipleVerifiers(t *testing.T) {
	env := newSvMgrTestEnv(t, 2)

	numTxs := 10
	numBlocks := 1000
	blocks := make([]*protoblocktx.Block, numBlocks)
	expectedValid := make([]*protoblocktx.Block, numBlocks)
	expectedInvalid := make([]*protoblocktx.Block, numBlocks)
	for i := 0; i < numBlocks; i++ {
		blocks[i], expectedValid[i], expectedInvalid[i] = createBlockForTest(t, i, numTxs)
	}
	for i := 0; i < numBlocks; i++ {
		env.inputBlock <- blocks[i]
	}

	deadline := time.After(10 * time.Second)
	// For each block in the input, we expect a block in each of the two outputs.
	for i := 0; i < numBlocks*2; i++ {
		select {
		case blk := <-env.outputBlockWithValidTxs:
			require.Equal(t, expectedValid[blk.Number], blk)
		case blk := <-env.outputBlockWithInvalidTxs:
			require.Equal(t, expectedInvalid[blk.Number], blk)
		case <-deadline:
			t.Fatal("Did not receive all blocks from output after timeout")
		}
	}

	totalBlocksReceived := uint32(0)
	for _, sv := range env.mockSvService {
		// Verify that each service got a reasonable proportion of the requests.
		totalBlocksReceived += sv.GetNumBlocksReceived()
		require.Greater(t, sv.GetNumBlocksReceived(), uint32(0.1*float32(numBlocks)))
	}
	require.Equal(t, uint32(numBlocks), totalBlocksReceived)
}

func TestSignatureVerifierManagerKey(t *testing.T) {
	env := newSvMgrTestEnv(t, 3)

	// verify that all mock sigverifiers have empty verification key
	for _, mockSv := range env.mockSvService {
		require.Empty(t, mockSv.GetVerificationKey())
	}

	// set verification key
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	err := env.signVerifierManager.setVerificationKey(
		ctx,
		&protosigverifierservice.Key{
			SerializedBytes: []byte("dummy"),
		},
	)
	require.NoError(t, err)

	// verify that all mock sigverifiers have the same verification key
	for _, mockSv := range env.mockSvService {
		require.Equal(t, []byte("dummy"), mockSv.GetVerificationKey())
	}
}

func TestSignatureVerifierWithAllInvalidTxs(t *testing.T) {
	env := newSvMgrTestEnv(t, 1)

	blk := &protoblocktx.Block{
		Number: 1,
		Txs: []*protoblocktx.Tx{
			{
				Id: "tx1",
			},
		},
	}
	env.inputBlock <- blk

	env.requireBlock(t, &protoblocktx.Block{Number: 1}, blk)
}

func createBlockForTest(
	_ *testing.T,
	blkNum, numTxs int,
) (*protoblocktx.Block, *protoblocktx.Block, *protoblocktx.Block) {
	block := &protoblocktx.Block{
		Number: uint64(blkNum),
	}

	blockWithValidTxs := &protoblocktx.Block{
		Number: uint64(blkNum),
	}

	blockWithInvalidTxs := &protoblocktx.Block{
		Number: uint64(blkNum),
	}

	for i := 0; i < numTxs; i++ {
		tx := &protoblocktx.Tx{}
		txNum := uint32(i) //nolint:gosec
		switch i % 2 {
		case 0:
			// even number txs are valid.
			tx.Signatures = [][]byte{[]byte("dummy")}
			blockWithValidTxs.Txs = append(blockWithValidTxs.Txs, tx)
			blockWithValidTxs.TxsNum = append(blockWithValidTxs.TxsNum, txNum)
		case 1:
			// odd number txs are invalid.
			blockWithInvalidTxs.Txs = append(blockWithInvalidTxs.Txs, tx)
			blockWithInvalidTxs.TxsNum = append(blockWithInvalidTxs.TxsNum, txNum)
		}

		block.Txs = append(block.Txs, tx)
		block.TxsNum = append(block.TxsNum, txNum)
	}

	return block, blockWithValidTxs, blockWithInvalidTxs
}

func TestSignatureVerifierManagerRecovery(t *testing.T) {
	env := newSvMgrTestEnv(t, 1)
	for _, sv := range env.mockSvService {
		sv.MockFaultyNodeDropSize = 1
	}

	blkNum := 0
	numTxs := 10
	blk, expectedBlkWithValTxs, expectedBlkWithInvalidTxs := createBlockForTest(t, blkNum, numTxs)
	env.inputBlock <- blk

	// Validate the full block have not been reported
	firstSv := env.signVerifierManager.signVerifier[0]
	require.Eventually(t, func() bool {
		v, ok := firstSv.resultAccumulator.Load(uint64(blkNum))
		if !ok {
			return false
		}
		blkWithResult, _ := v.(*blockWithResult) // nolint:revive
		return blkWithResult.pendingResultCount < len(blkWithResult.block.Txs)
	}, 4*time.Second, 100*time.Millisecond)
	require.Empty(t, env.outputBlockWithValidTxs)
	require.Empty(t, env.outputBlockWithInvalidTxs)

	for _, s := range env.grpcServers {
		s.Stop()
	}
	time.Sleep(time.Second)

	for _, sv := range env.mockSvService {
		sv.MockFaultyNodeDropSize = 0
	}
	env.grpcServers = sigverifiermock.StartMockSVServiceFromListWithConfig(
		env.mockSvService, env.serversConfig,
	)

	env.requireBlock(t, expectedBlkWithValTxs, expectedBlkWithInvalidTxs)
}
