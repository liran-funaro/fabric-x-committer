package coordinatorservice

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type svMgrTestEnv struct {
	signVerifierManager       *signatureVerifierManager
	inputBlock                chan *protoblocktx.Block
	outputBlockWithValidTxs   chan *protoblocktx.Block
	outputBlockWithInvalidTxs chan *protoblocktx.Block
	mockSvService             []*sigverifiermock.MockSigVerifier
}

func newSvMgrTestEnv(t *testing.T, numSvService int) *svMgrTestEnv {
	sc, svs, grpcSrvs := sigverifiermock.StartMockSVService(numSvService)

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
	errChan, err := svm.start()
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		numErrorableGoroutines := 2 * len(svs)
		for i := 0; i < numErrorableGoroutines; i++ {
			require.NoError(t, <-errChan)
		}
		wg.Done()
	}()

	t.Cleanup(func() {
		require.NoError(t, svm.close())
		close(inputBlock)
		close(outputBlockWithValidTxs)
		close(outputBlockWithInvalidTxs)
		for _, mockSv := range svs {
			mockSv.Close()
		}
		wg.Wait()
		close(errChan)
		for _, s := range grpcSrvs {
			s.GracefulStop()
		}
	})

	return &svMgrTestEnv{
		signVerifierManager:       svm,
		inputBlock:                inputBlock,
		outputBlockWithValidTxs:   outputBlockWithValidTxs,
		outputBlockWithInvalidTxs: outputBlockWithInvalidTxs,
		mockSvService:             svs,
	}
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

	require.Equal(t, expectedBlkWithValTxs, <-env.outputBlockWithValidTxs)
	require.Equal(t, expectedBlkWithInvalTxs, <-env.outputBlockWithInvalidTxs)

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

	require.Equal(t, expectedBlkWithValTxs, <-env.outputBlockWithValidTxs)
	require.Equal(t, expectedBlkWithInvalTxs, <-env.outputBlockWithInvalidTxs)

	require.Eventually(t, func() bool {
		return test.GetMetricValue(
			t,
			env.signVerifierManager.config.metrics.sigverifierTransactionProcessedTotal,
		) == 15
	}, 2*time.Second, 100*time.Millisecond)
}

func TestSignatureVerifierManagerWithMultipleVerifiers(t *testing.T) {
	env := newSvMgrTestEnv(t, 2)

	blkNum := 1
	numTxs := 10
	blk1, expectedBlk1WithValTxs, expectedBlk1WithInvalTxs := createBlockForTest(t, blkNum, numTxs)
	blkNum = 2
	blk2, expectedBlk2WithValTxs, expectedBlk2WithInvalTxs := createBlockForTest(t, blkNum, numTxs)

	require.Eventually(t, func() bool {
		env.inputBlock <- blk1
		env.inputBlock <- blk2

		var outputBlk1ValTxs, outputBlk2ValTxs *protoblocktx.Block
		var outputBlk1InvalTxs, outputBlk2InvalTxs *protoblocktx.Block

		blk := <-env.outputBlockWithValidTxs
		switch blk.Number {
		case 1:
			outputBlk1ValTxs = blk
			outputBlk2ValTxs = <-env.outputBlockWithValidTxs
		case 2:
			outputBlk2ValTxs = blk
			outputBlk1ValTxs = <-env.outputBlockWithValidTxs
		}

		blk = <-env.outputBlockWithInvalidTxs
		switch blk.Number {
		case 1:
			outputBlk1InvalTxs = blk
			outputBlk2InvalTxs = <-env.outputBlockWithInvalidTxs
		case 2:
			outputBlk2InvalTxs = blk
			outputBlk1InvalTxs = <-env.outputBlockWithInvalidTxs
		}

		require.Equal(t, expectedBlk1WithValTxs, outputBlk1ValTxs)
		require.Equal(t, expectedBlk2WithValTxs, outputBlk2ValTxs)

		require.Equal(t, expectedBlk1WithInvalTxs, outputBlk1InvalTxs)
		require.Equal(t, expectedBlk2WithInvalTxs, outputBlk2InvalTxs)

		for _, sv := range env.mockSvService {
			if sv.GetNumBlocksReceived() == 0 {
				return false
			}
		}
		return true
	}, 4*time.Second, 100*time.Millisecond)
}

func TestSignatureVerifierManagerKey(t *testing.T) {
	env := newSvMgrTestEnv(t, 3)

	// verify that all mock sigverifiers have empty verification key
	for _, mockSv := range env.mockSvService {
		require.Empty(t, mockSv.GetVerificationKey())
	}

	// set verification key
	err := env.signVerifierManager.setVerificationKey(
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

	require.Equal(t, &protoblocktx.Block{
		Number: 1,
	}, <-env.outputBlockWithValidTxs)
	require.Equal(t, blk, <-env.outputBlockWithInvalidTxs)
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

		switch i % 2 {
		case 0:
			// even number txs are valid.
			tx.Signatures = [][]byte{[]byte("dummy")}
			blockWithValidTxs.Txs = append(blockWithValidTxs.Txs, tx)
		case 1:
			// odd number txs are invalid.
			blockWithInvalidTxs.Txs = append(blockWithInvalidTxs.Txs, tx)
		}

		block.Txs = append(block.Txs, tx)
	}

	return block, blockWithValidTxs, blockWithInvalidTxs
}
