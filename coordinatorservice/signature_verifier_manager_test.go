package coordinatorservice

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/policy"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type svMgrTestEnv struct {
	signVerifierManager *signatureVerifierManager
	inputTxBatch        chan dependencygraph.TxNodeBatch
	outputValidatedTxs  chan dependencygraph.TxNodeBatch
	mockSvService       []*mock.SigVerifier
	grpcServers         *test.GrpcServers
}

func newSvMgrTestEnv(t *testing.T, numSvService int, expectedEndErrorMsg ...byte) *svMgrTestEnv {
	t.Helper()
	expectedEndError := string(expectedEndErrorMsg)
	svs, sc := mock.StartMockSVService(t, numSvService)

	inputTxBatch := make(chan dependencygraph.TxNodeBatch, 10)
	outputValidatedTxs := make(chan dependencygraph.TxNodeBatch, 10)

	svm := newSignatureVerifierManager(
		&signVerifierManagerConfig{
			serversConfig:            sc.Configs,
			incomingTxsForValidation: inputTxBatch,
			outgoingValidatedTxs:     outputValidatedTxs,
			metrics:                  newPerformanceMetrics(),
		},
	)

	test.RunServiceForTest(t.Context(), t,
		func(ctx context.Context) error {
			if expectedEndError != "" {
				require.ErrorContains(t, connection.FilterStreamRPCError(svm.run(ctx)), expectedEndError)
			} else {
				assert.NoError(t, svm.run(ctx))
			}
			return nil
		},
		svm.connectionReady.WaitForReady,
	)

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
	t.Helper()
	select {
	case actualTxBatch := <-e.outputValidatedTxs:
		require.ElementsMatch(t, expectedValidatedTxs, actualTxBatch)
	case <-time.After(5 * time.Second):
		t.Fatal("Did not receive block from output after timeout")
	}
}

func TestSignatureVerifierManagerWithSingleVerifier(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	env := newSvMgrTestEnv(t, 2)

	numTxs := 10
	numBlocks := 1000
	txBatches := make([]dependencygraph.TxNodeBatch, numBlocks)
	expectedValidatedTxs := make([]dependencygraph.TxNodeBatch, numBlocks)
	for i := range numBlocks {
		txBatches[i], expectedValidatedTxs[i] = createTxNodeBatchForTest(t, i, numTxs)
	}
	for i := range numBlocks {
		env.inputTxBatch <- txBatches[i]
	}

	deadline := time.After(10 * time.Second)
	// For each input batch in the input, we expect a batch in each of the two outputs.
	for range numBlocks {
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
		require.Empty(t, sv.txBeingValidated)
		sv.txMu.Unlock()
	}
}

func TestSignatureVerifierManagerKey(t *testing.T) {
	t.Parallel()
	env := newSvMgrTestEnv(t, 3)

	// verify that all mock policy verifiers have empty verification key.
	for _, mockSv := range env.mockSvService {
		require.Empty(t, mockSv.GetPolicies().Policies)
	}

	// set verification key
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	nsID := "0"
	expectedPolicy := &protoblocktx.Policies{
		Policies: []*protoblocktx.PolicyItem{
			policy.MakePolicy(t, nsID, &protoblocktx.NamespacePolicy{
				Scheme:    signature.Ecdsa,
				PublicKey: []byte("dummy"),
			}),
		},
	}
	err := env.signVerifierManager.updatePolicies(ctx, expectedPolicy)
	require.NoError(t, err)

	// verify that all mock policy verifiers have the same verification key.
	for _, mockSv := range env.mockSvService {
		require.True(t, proto.Equal(expectedPolicy, mockSv.GetPolicies()))
	}
}

func TestSignatureVerifierWithAllInvalidTxs(t *testing.T) {
	t.Parallel()
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
	for i := range numTxs {
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
	t.Parallel()
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

	for i, s := range env.grpcServers.Servers {
		s.Stop()
		test.CheckServerStopped(t, env.grpcServers.Configs[i].Endpoint.Address())
	}

	for _, sv := range env.mockSvService {
		sv.MockFaultyNodeDropSize = 0
	}
	env.grpcServers = mock.StartMockSVServiceFromListWithConfig(
		t, env.mockSvService, env.grpcServers.Configs,
	)

	env.requireTxBatch(t, expectedValidatedTxs[4:])
	env.requireTxBatch(t, expectedValidatedTxs[:4])
}

func TestSignatureVerifierManagerPolicyUpdateRecovery(t *testing.T) {
	t.Parallel()
	env := newSvMgrTestEnv(t, 1, []byte("invalid argument")...)

	env.grpcServers.Servers[0].Stop()
	test.CheckServerStopped(t, env.grpcServers.Configs[0].Endpoint.Address())

	allNsPolicies := env.signVerifierManager.signVerifier[0].allNsPolicies
	require.Empty(t, allNsPolicies)
	require.False(t, env.signVerifierManager.signVerifier[0].pendingNsPolicies)
	ns1Policy, _ := policy.MakePolicyAndNsSigner(t, "ns1")
	ns2Policy, _ := policy.MakePolicyAndNsSigner(t, "ns2")
	require.NoError(t, env.signVerifierManager.updatePolicies(t.Context(), &protoblocktx.Policies{
		Policies: []*protoblocktx.PolicyItem{ns1Policy, ns2Policy},
	}))

	allNsPolicies = env.signVerifierManager.signVerifier[0].allNsPolicies
	require.Len(t, allNsPolicies, 2)
	require.Equal(t, ns1Policy, allNsPolicies["ns1"])
	require.Equal(t, ns2Policy, allNsPolicies["ns2"])
	require.True(t, env.signVerifierManager.signVerifier[0].pendingNsPolicies)

	ns2NewPolicy, _ := policy.MakePolicyAndNsSigner(t, "ns2")
	require.NoError(t, env.signVerifierManager.updatePolicies(t.Context(), &protoblocktx.Policies{
		Policies: []*protoblocktx.PolicyItem{ns2NewPolicy},
	}))

	require.Len(t, allNsPolicies, 2)
	require.Equal(t, ns1Policy, allNsPolicies["ns1"])
	require.Equal(t, ns2NewPolicy, allNsPolicies["ns2"])
	require.True(t, env.signVerifierManager.signVerifier[0].pendingNsPolicies)

	blkNum := 0
	numTxs := 10
	txBatch, expectedValidatedTxs := createTxNodeBatchForTest(t, blkNum, numTxs)
	env.inputTxBatch <- txBatch

	require.Never(t, func() bool {
		return !env.signVerifierManager.signVerifier[0].pendingNsPolicies
	}, 2*time.Second, 1*time.Second)

	env.grpcServers = mock.StartMockSVServiceFromListWithConfig(
		t, env.mockSvService, env.grpcServers.Configs,
	)

	require.Eventually(t, func() bool {
		return !env.signVerifierManager.signVerifier[0].pendingNsPolicies
	}, 3*time.Second, 250*time.Millisecond)

	env.requireTxBatch(t, expectedValidatedTxs)

	env.mockSvService[0].SetReturnErrorForUpdatePolicies(true)

	// empty policies never reach the verifier
	require.NoError(t, env.signVerifierManager.updatePolicies(t.Context(), &protoblocktx.Policies{}))

	require.Error(t, env.signVerifierManager.updatePolicies(t.Context(), &protoblocktx.Policies{
		Policies: []*protoblocktx.PolicyItem{
			{
				Namespace: "$$$",
			},
		},
	}))
	require.True(t, env.signVerifierManager.signVerifier[0].pendingNsPolicies)

	env.grpcServers.Servers[0].Stop()
	test.CheckServerStopped(t, env.grpcServers.Configs[0].Endpoint.Address())
	env.grpcServers = mock.StartMockSVServiceFromListWithConfig(
		t, env.mockSvService, env.grpcServers.Configs,
	)

	// When the server is restarted, it would try to send all policies including the
	// bad policy. As a result, the svm would fail with an invalid argument error.
	// It would not even reach the location where it sends the pendingNsPolicies.
	require.Never(t, func() bool {
		return !env.signVerifierManager.signVerifier[0].pendingNsPolicies
	}, 2*time.Second, 500*time.Millisecond)
}
