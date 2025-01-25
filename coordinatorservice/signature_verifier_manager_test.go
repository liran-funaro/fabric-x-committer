package coordinatorservice

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type svMgrTestEnv struct {
	signVerifierManager *signatureVerifierManager
	inputTxBatch        chan dependencygraph.TxNodeBatch
	outputValidatedTxs  chan dependencygraph.TxNodeBatch
	mockSvService       []*mock.SigVerifier
	grpcServers         *test.GrpcServers
}

func newSvMgrTestEnv(t *testing.T, numSvService int) *svMgrTestEnv {
	svs, sc := mock.StartMockSVService(t, numSvService)

	inputTxBatch := make(chan dependencygraph.TxNodeBatch, 10)
	outputValidatedTxs := make(chan dependencygraph.TxNodeBatch, 10)

	svm := newSignatureVerifierManager(
		&signVerifierManagerConfig{
			serversConfig:            sc.Configs,
			incomingTxsForValidation: inputTxBatch,
			outgoingValidatedTxs:     outputValidatedTxs,
			metrics:                  newPerformanceMetrics(true),
		},
	)

	test.RunServiceForTest(t, svm.run, func(ctx context.Context) bool {
		select {
		case <-ctx.Done():
			return false
		case <-svm.connectionReady:
			return true
		}
	})

	env := &svMgrTestEnv{
		signVerifierManager: svm,
		inputTxBatch:        inputTxBatch,
		outputValidatedTxs:  outputValidatedTxs,
		mockSvService:       svs,
		grpcServers:         sc,
	}

	return env
}

func (e *svMgrTestEnv) requireTxBatch(t *testing.T, expectedValidatedTxs dependencygraph.TxNodeBatch) {
	select {
	case actualTxBatch := <-e.outputValidatedTxs:
		require.ElementsMatch(t, expectedValidatedTxs, actualTxBatch)
	case <-time.After(5 * time.Second):
		t.Fatal("Did not receive block from output after timeout")
	}
}

func TestSignatureVerifierManagerWithSingleVerifier(t *testing.T) {
	// SigVerifier marks valid and invalid flag as follows:
	// - when the block number is even, the even numbered txs are valid and the odd numbered txs are invalid
	// - when the block number is odd, the even numbered txs are invalid and the odd numbered txs are valid
	env := newSvMgrTestEnv(t, 1)

	blkNum := 0
	numTxs := 10
	txBatch, expectedValidatedTxs := createTxNodeBatchForTest(t, blkNum, numTxs)
	env.inputTxBatch <- txBatch

	env.requireTxBatch(t, expectedValidatedTxs)

	blkNum = 1
	numTxs = 1
	txBatch, expectedValidatedTxs = createTxNodeBatchForTest(t, blkNum, numTxs)
	env.inputTxBatch <- txBatch

	env.requireTxBatch(t, expectedValidatedTxs)

	blkNum = 2
	numTxs = 4
	txBatch, expectedValidatedTxs = createTxNodeBatchForTest(t, blkNum, numTxs)
	env.inputTxBatch <- txBatch

	env.requireTxBatch(t, expectedValidatedTxs)

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
	txBatches := make([]dependencygraph.TxNodeBatch, numBlocks)
	expectedValidatedTxs := make([]dependencygraph.TxNodeBatch, numBlocks)
	for i := 0; i < numBlocks; i++ {
		txBatches[i], expectedValidatedTxs[i] = createTxNodeBatchForTest(t, i, numTxs)
	}
	for i := 0; i < numBlocks; i++ {
		env.inputTxBatch <- txBatches[i]
	}

	deadline := time.After(10 * time.Second)
	// For each input batch in the input, we expect a batch in each of the two outputs.
	for i := 0; i < numBlocks; i++ {
		select {
		case txBatch := <-env.outputValidatedTxs:
			require.ElementsMatch(t, expectedValidatedTxs[txBatch[0].Tx.BlockNumber], txBatch)
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

	for _, sv := range env.signVerifierManager.signVerifier {
		sv.txMu.Lock()
		require.Len(t, sv.txBeingValidated, 0)
		sv.txMu.Unlock()
	}
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
	txBatch := dependencygraph.TxNodeBatch{}
	expectedValidatedTxs := dependencygraph.TxNodeBatch{}
	for i := range 3 {
		txNode := &dependencygraph.TransactionNode{
			Tx: &protovcservice.Transaction{
				BlockNumber: uint64(i), // nolint:gosec
				TxNum:       uint32(i), // nolint:gosec
			},
		}
		txBatch = append(txBatch, txNode)

		expectedValidatedTxs = append(expectedValidatedTxs, &dependencygraph.TransactionNode{
			Tx: &protovcservice.Transaction{
				BlockNumber:           txNode.Tx.BlockNumber,
				TxNum:                 txNode.Tx.TxNum,
				PrelimInvalidTxStatus: sigInvalidTxStatus,
			},
		})
	}

	env := newSvMgrTestEnv(t, 1)
	env.inputTxBatch <- txBatch
	env.requireTxBatch(t, expectedValidatedTxs)
}

func createTxNodeBatchForTest(
	_ *testing.T,
	blkNum, numTxs int,
) (inputTxBatch, expectedValidatedTxs dependencygraph.TxNodeBatch) {
	for i := 0; i < numTxs; i++ {
		txNode := &dependencygraph.TransactionNode{
			Tx: &protovcservice.Transaction{
				BlockNumber: uint64(blkNum), //nolint:gosec
				TxNum:       uint32(i),      //nolint:gosec
			},
		}

		switch i % 2 {
		case 0:
			// even number txs are valid.
			txNode.Signatures = [][]byte{[]byte("dummy")}
			expectedValidatedTxs = append(expectedValidatedTxs, txNode)
		case 1:
			// odd number txs are invalid. No signature means invalid transaction.
			// we need to create a copy of txNode to add expected status.
			txNodeWithStatus := &dependencygraph.TransactionNode{
				Tx: &protovcservice.Transaction{
					BlockNumber:           txNode.Tx.BlockNumber,
					TxNum:                 txNode.Tx.TxNum,
					PrelimInvalidTxStatus: sigInvalidTxStatus,
				},
			}
			expectedValidatedTxs = append(expectedValidatedTxs, txNodeWithStatus)
		}

		inputTxBatch = append(inputTxBatch, txNode)
	}

	return inputTxBatch, expectedValidatedTxs
}

func TestSignatureVerifierManagerRecovery(t *testing.T) {
	env := newSvMgrTestEnv(t, 1)
	for _, sv := range env.mockSvService {
		sv.MockFaultyNodeDropSize = 4
	}

	blkNum := 0
	numTxs := 10
	txBatch, expectedValidatedTxs := createTxNodeBatchForTest(t, blkNum, numTxs)
	env.inputTxBatch <- txBatch

	// Validate the full block have not been reported
	firstSv := env.signVerifierManager.signVerifier[0]
	require.Eventually(t, func() bool {
		firstSv.txMu.Lock()
		defer firstSv.txMu.Unlock()
		return len(firstSv.txBeingValidated) == numTxs-6 // first 4 transactions would be pending
	}, 4*time.Second, 100*time.Millisecond)

	for _, s := range env.grpcServers.Servers {
		s.Stop()
	}
	time.Sleep(time.Second)

	for _, sv := range env.mockSvService {
		sv.MockFaultyNodeDropSize = 0
	}
	env.grpcServers = mock.StartMockSVServiceFromListWithConfig(
		t, env.mockSvService, env.grpcServers.Configs,
	)

	env.requireTxBatch(t, expectedValidatedTxs[4:])
	env.requireTxBatch(t, expectedValidatedTxs[:4])
}
