package coordinatorservice

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/protobuf/proto"
)

type vcMgrTestEnv struct {
	validatorCommitterManager *validatorCommitterManager
	inputTxs                  chan dependencygraph.TxNodeBatch
	outputTxs                 chan dependencygraph.TxNodeBatch
	outputTxsStatus           chan *protoblocktx.TransactionsStatus
	mockVcServices            []*mock.VcService
	mockVCGrpcServers         *test.GrpcServers
	sigVerTestEnv             *svMgrTestEnv
}

func newVcMgrTestEnv(t *testing.T, numVCService int, expectedEndErrorMsg ...byte) *vcMgrTestEnv {
	t.Helper()
	expectedEndError := string(expectedEndErrorMsg)
	vcs, servers := mock.StartMockVCService(t, numVCService)
	svEnv := newSvMgrTestEnv(t, 2)

	inputTxs := make(chan dependencygraph.TxNodeBatch, 10)
	outputTxs := make(chan dependencygraph.TxNodeBatch, 10)
	outputTxsStatus := make(chan *protoblocktx.TransactionsStatus, 10)

	vcm := newValidatorCommitterManager(
		&validatorCommitterManagerConfig{
			serversConfig:                  servers.Configs,
			incomingTxsForValidationCommit: inputTxs,
			outgoingValidatedTxsNode:       outputTxs,
			outgoingTxsStatus:              outputTxsStatus,
			metrics:                        newPerformanceMetrics(true),
			policyMgr:                      &policyManager{signVerifierMgr: svEnv.signVerifierManager},
		},
	)

	test.RunServiceForTest(t.Context(), t, func(ctx context.Context) error {
		err := connection.FilterStreamRPCError(vcm.run(ctx))
		logger.ErrorStackTrace(err)
		if expectedEndError != "" {
			require.ErrorContains(t, err, expectedEndError)
		} else {
			assert.NoError(t, err)
		}
		return nil
	}, func(ctx context.Context) bool {
		select {
		case <-ctx.Done():
			return false
		case <-vcm.connectionReady:
			return true
		}
	})

	return &vcMgrTestEnv{
		validatorCommitterManager: vcm,
		inputTxs:                  inputTxs,
		outputTxs:                 outputTxs,
		outputTxsStatus:           outputTxsStatus,
		mockVcServices:            vcs,
		mockVCGrpcServers:         servers,
		sigVerTestEnv:             svEnv,
	}
}

func TestValidatorCommitterManager(t *testing.T) { //nolint:gocognit
	t.Parallel()

	ensureZeroWaitingTxs := func(env *vcMgrTestEnv) {
		for _, vc := range env.validatorCommitterManager.validatorCommitter {
			count := 0
			vc.txBeingValidated.Range(func(_, _ any) bool {
				count++
				return true
			})
			require.Zero(t, count)
		}
	}

	t.Run("Send tx batch to use any vcservice", func(t *testing.T) {
		t.Parallel()
		env := newVcMgrTestEnv(t, 2)
		txBatch, expectedTxsStatus := createInputTxsNodeForTest(t, 5, 1, 1)
		env.inputTxs <- txBatch

		outTxs := <-env.outputTxs
		require.ElementsMatch(t, txBatch, outTxs)

		outTxsStatus := <-env.outputTxsStatus

		require.Equal(t, expectedTxsStatus.Status, outTxsStatus.Status)

		require.Eventually(t, func() bool {
			return test.GetMetricValue(
				t,
				env.validatorCommitterManager.config.metrics.vcserviceTransactionProcessedTotal,
			) == 5
		}, 2*time.Second, 100*time.Millisecond)
		ensureZeroWaitingTxs(env)
	})

	t.Run("send batches to ensure all vcservices are used", func(t *testing.T) {
		t.Parallel()
		env := newVcMgrTestEnv(t, 2)
		txBatch1, expectedTxsStatus1 := createInputTxsNodeForTest(t, 5, 1, 2)
		txBatch2, expectedTxsStatus2 := createInputTxsNodeForTest(t, 5, 6, 3)

		require.Eventually(t, func() bool {
			env.inputTxs <- txBatch1
			env.inputTxs <- txBatch2

			outputTxBatch1 := <-env.outputTxs
			outputTxBatch2 := <-env.outputTxs

			outTxsStatus1 := <-env.outputTxsStatus
			outTxsStatus2 := <-env.outputTxsStatus

			require.ElementsMatch(
				t,
				append(txBatch1, txBatch2...),
				append(outputTxBatch1, outputTxBatch2...),
			)

			mergeTxsStatus := func(
				txsStatus1,
				txsStatus2 *protoblocktx.TransactionsStatus,
			) map[string]*protoblocktx.StatusWithHeight {
				txsStatus := make(map[string]*protoblocktx.StatusWithHeight)

				for id, status := range txsStatus1.Status {
					txsStatus[id] = status
				}
				for id, status := range txsStatus2.Status {
					txsStatus[id] = status
				}

				return txsStatus
			}

			require.Equal(
				t,
				mergeTxsStatus(expectedTxsStatus1, expectedTxsStatus2),
				mergeTxsStatus(outTxsStatus1, outTxsStatus2),
			)

			for _, vc := range env.mockVcServices {
				if vc.GetNumBatchesReceived() == 0 {
					return false
				}
			}
			return true
		}, 4*time.Second, 100*time.Millisecond)
		ensureZeroWaitingTxs(env)
	})

	t.Run("namespace transaction should update signature verifier", func(t *testing.T) {
		t.Parallel()
		env := newVcMgrTestEnv(t, 2)
		for _, mockSvService := range env.sigVerTestEnv.mockSvService {
			require.Empty(t, mockSvService.GetPolicies().Policies)
		}

		verificationKey, _ := workload.NewHashSignerVerifier(&workload.Policy{
			Scheme: signature.Ecdsa,
			Seed:   10,
		}).GetVerificationKeyAndSigner()
		p := &protoblocktx.NamespacePolicy{
			Scheme:    signature.Ecdsa,
			PublicKey: verificationKey,
		}
		pBytes, err := proto.Marshal(p)
		require.NoError(t, err)

		txBatch := dependencygraph.TxNodeBatch{
			{
				Tx: &protovcservice.Transaction{
					ID: "create ns 1",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId: types.MetaNamespaceID,
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key:   []byte("1"),
									Value: pBytes,
								},
							},
						},
					},
					BlockNumber: uint64(100),
					TxNum:       uint32(64),
				},
			},
		}
		env.inputTxs <- txBatch

		outTxsStatus := <-env.outputTxsStatus

		require.Len(t, outTxsStatus.Status, 1)
		require.Equal(t, types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 100, 64),
			outTxsStatus.Status["create ns 1"])

		require.Equal(t, txBatch, <-env.outputTxs)

		for _, mockSvService := range env.sigVerTestEnv.mockSvService {
			require.ElementsMatch(t, []*protoblocktx.PolicyItem{
				{
					Namespace: "1",
					Policy:    pBytes,
				},
			}, mockSvService.GetPolicies().Policies)
		}
		ensureZeroWaitingTxs(env)
	})

	t.Run("corrupted policy would crash the vc manager", func(t *testing.T) {
		t.Parallel()
		env := newVcMgrTestEnv(t, 2, []byte("invalid argument")...)
		for _, mockSvService := range env.sigVerTestEnv.mockSvService {
			mockSvService.SetReturnErrorForUpdatePolicies(true)
		}

		txBatch := dependencygraph.TxNodeBatch{
			{
				Tx: &protovcservice.Transaction{
					ID: "create ns 1",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId: types.MetaNamespaceID,
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key:   []byte("1"),
									Value: []byte("policy"),
								},
							},
						},
					},
					BlockNumber: uint64(100),
					TxNum:       uint32(64),
				},
			},
		}
		env.inputTxs <- txBatch
		time.Sleep(2 * time.Second)
	})
}

func TestValidatorCommitterManagerRecovery(t *testing.T) {
	t.Parallel()
	env := newVcMgrTestEnv(t, 1)
	env.mockVcServices[0].MockFaultyNodeDropSize = 4

	numTxs := 10
	txBatch, expectedTxsStatus := createInputTxsNodeForTest(t, numTxs, 0, 0)
	env.inputTxs <- txBatch

	require.Eventually(t, func() bool {
		count := 0
		env.validatorCommitterManager.validatorCommitter[0].txBeingValidated.Range(func(_, _ any) bool {
			count++
			return true
		})
		return count == numTxs-6
	}, 4*time.Second, 100*time.Millisecond)

	env.mockVCGrpcServers.Servers[0].Stop()
	time.Sleep(2 * time.Second) // replace with test.CheckServerStopped

	env.mockVcServices[0].MockFaultyNodeDropSize = 0
	env.mockVCGrpcServers = mock.StartMockVCServiceFromListWithConfig(
		t, env.mockVcServices, env.mockVCGrpcServers.Configs,
	)

	actualTxsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	for range 2 {
		result := <-env.outputTxsStatus
		for txID, height := range result.Status {
			actualTxsStatus[txID] = height
		}
	}
	require.Equal(t, expectedTxsStatus.Status, actualTxsStatus)
}

func createInputTxsNodeForTest(_ *testing.T, numTxs, startIndex int, blkNum uint64) (
	[]*dependencygraph.TransactionNode, *protoblocktx.TransactionsStatus,
) {
	txsNode := make([]*dependencygraph.TransactionNode, numTxs)
	expectedTxsStatus := &protoblocktx.TransactionsStatus{
		Status: make(map[string]*protoblocktx.StatusWithHeight),
	}

	for i := range numTxs {
		id := "tx" + strconv.Itoa(startIndex+i)
		txsNode[i] = &dependencygraph.TransactionNode{
			Tx: &protovcservice.Transaction{
				ID:          id,
				BlockNumber: blkNum,
				TxNum:       uint32(i), //nolint:gosec
			},
		}
		expectedTxsStatus.Status[id] = types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, blkNum, i)
	}

	return txsNode, expectedTxsStatus
}
