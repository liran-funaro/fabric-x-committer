/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package coordinator

import (
	"context"
	"maps"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/hyperledger/fabric-x-committer/api/protoblocktx"
	"github.com/hyperledger/fabric-x-committer/api/protosigverifierservice"
	"github.com/hyperledger/fabric-x-committer/api/protovcservice"
	"github.com/hyperledger/fabric-x-committer/api/types"
	"github.com/hyperledger/fabric-x-committer/loadgen/workload"
	"github.com/hyperledger/fabric-x-committer/mock"
	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring"
	"github.com/hyperledger/fabric-x-committer/utils/signature"
	"github.com/hyperledger/fabric-x-committer/utils/test"
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

func newVcMgrTestEnv(t *testing.T, numVCService int) *vcMgrTestEnv {
	t.Helper()
	vcs, servers := mock.StartMockVCService(t, numVCService)
	svEnv := newSvMgrTestEnv(t, 2)

	inputTxs := make(chan dependencygraph.TxNodeBatch, 10)
	outputTxs := make(chan dependencygraph.TxNodeBatch, 10)
	outputTxsStatus := make(chan *protoblocktx.TransactionsStatus, 10)

	vcm := newValidatorCommitterManager(
		&validatorCommitterManagerConfig{
			clientConfig:                   test.ServerToClientConfig(servers.Configs...),
			incomingTxsForValidationCommit: inputTxs,
			outgoingValidatedTxsNode:       outputTxs,
			outgoingTxsStatus:              outputTxsStatus,
			metrics:                        newPerformanceMetrics(),
			policyMgr:                      svEnv.policyManager,
		},
	)

	test.RunServiceForTest(t.Context(), t, func(ctx context.Context) error {
		err := connection.FilterStreamRPCError(vcm.run(ctx))
		assert.NoError(t, err)
		return nil
	}, vcm.ready.WaitForReady)

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

func (e *vcMgrTestEnv) requireConnectionMetrics(
	t *testing.T,
	vcIndex, expectedConnStatus, expectedConnFailureTotal int,
) {
	t.Helper()
	require.Less(t, vcIndex, len(e.validatorCommitterManager.validatorCommitter))
	sv := e.validatorCommitterManager.validatorCommitter[vcIndex]
	monitoring.RequireConnectionMetrics(
		t, sv.conn.CanonicalTarget(),
		sv.metrics.vcservicesConnection,
		monitoring.ExpectedConn{Status: expectedConnStatus, FailureTotal: expectedConnFailureTotal},
	)
}

func (e *vcMgrTestEnv) requireRetriedTxsTotal(t *testing.T, expectedRetriedTxsTotal int) {
	t.Helper()
	test.EventuallyIntMetric(
		t, expectedRetriedTxsTotal, e.validatorCommitterManager.config.metrics.vcservicesRetriedTransactionTotal,
		5*time.Second, 250*time.Millisecond,
	)
}

func TestValidatorCommitterManager(t *testing.T) {
	t.Parallel()

	ensureZeroWaitingTxs := func(env *vcMgrTestEnv) {
		for _, vc := range env.validatorCommitterManager.validatorCommitter {
			require.Zero(t, vc.txBeingValidated.Count())
		}
	}

	t.Run("Send tx batch to use any vcservice", func(t *testing.T) {
		t.Parallel()
		env := newVcMgrTestEnv(t, 2)
		txBatch, expectedTxsStatus := createInputTxsNodeForTest(5, 1, 1)
		env.inputTxs <- txBatch

		outTxs := <-env.outputTxs
		require.ElementsMatch(t, txBatch, outTxs)

		outTxsStatus := <-env.outputTxsStatus

		require.Equal(t, expectedTxsStatus.Status, outTxsStatus.Status)

		test.EventuallyIntMetric(
			t, 5, env.validatorCommitterManager.config.metrics.vcserviceTransactionProcessedTotal,
			2*time.Second, 100*time.Millisecond,
		)
		ensureZeroWaitingTxs(env)
	})

	t.Run("send batches to ensure all vcservices are used", func(t *testing.T) {
		t.Parallel()
		env := newVcMgrTestEnv(t, 2)
		txBatch1, expectedTxsStatus1 := createInputTxsNodeForTest(5, 1, 2)
		txBatch2, expectedTxsStatus2 := createInputTxsNodeForTest(5, 6, 3)

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
				maps.Copy(txsStatus, txsStatus1.Status)
				maps.Copy(txsStatus, txsStatus2.Status)
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
			require.Empty(t, mockSvService.GetUpdates())
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

		configBlock, err := workload.CreateDefaultConfigBlock(&workload.ConfigBlock{
			MetaNamespaceVerificationKey: verificationKey,
		})
		require.NoError(t, err)

		txBatch := []*dependencygraph.TransactionNode{
			{
				Tx: &protovcservice.Transaction{
					ID: "create config",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId: types.ConfigNamespaceID,
							BlindWrites: []*protoblocktx.Write{
								{
									Key:   []byte(types.ConfigKey),
									Value: configBlock.Data.Data[0],
								},
							},
						},
					},
					BlockNumber: uint64(100),
					TxNum:       uint32(63),
				},
			},
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

		require.Len(t, outTxsStatus.Status, 2)
		require.Equal(t,
			types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 100, 63),
			outTxsStatus.Status["create config"],
		)
		require.Equal(t,
			types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 100, 64),
			outTxsStatus.Status["create ns 1"],
		)

		require.ElementsMatch(t, txBatch, <-env.outputTxs)

		expectedUpdate := &protosigverifierservice.Update{
			Config: &protoblocktx.ConfigTransaction{
				Envelope: configBlock.Data.Data[0],
			},
			NamespacePolicies: &protoblocktx.NamespacePolicies{
				Policies: []*protoblocktx.PolicyItem{
					{
						Namespace: "1",
						Policy:    pBytes,
					},
				},
			},
		}
		update, _ := env.sigVerTestEnv.policyManager.getAll()
		requireUpdateEqual(t, expectedUpdate, update)
		ensureZeroWaitingTxs(env)
	})
}

func TestValidatorCommitterManagerRecovery(t *testing.T) {
	t.Parallel()
	env := newVcMgrTestEnv(t, 1)
	env.mockVcServices[0].MockFaultyNodeDropSize = 4

	env.requireConnectionMetrics(t, 0, connection.Connected, 0)
	env.requireRetriedTxsTotal(t, 0)

	numTxs := 10
	txBatch, expectedTxsStatus := createInputTxsNodeForTest(numTxs, 0, 0)
	env.inputTxs <- txBatch

	require.Eventually(t, func() bool {
		count := env.validatorCommitterManager.validatorCommitter[0].txBeingValidated.Count()
		return count == numTxs-6
	}, 4*time.Second, 100*time.Millisecond)

	env.mockVCGrpcServers.Servers[0].Stop()
	test.CheckServerStopped(t, env.mockVCGrpcServers.Configs[0].Endpoint.Address())
	env.requireConnectionMetrics(t, 0, connection.Disconnected, 1)

	env.mockVcServices[0].MockFaultyNodeDropSize = 0
	env.mockVCGrpcServers = mock.StartMockVCServiceFromListWithConfig(
		t, env.mockVcServices, env.mockVCGrpcServers.Configs,
	)
	env.requireConnectionMetrics(t, 0, connection.Connected, 1)
	env.requireRetriedTxsTotal(t, 4)

	actualTxsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	for range 2 {
		result := <-env.outputTxsStatus
		maps.Copy(actualTxsStatus, result.Status)
	}
	require.Equal(t, expectedTxsStatus.Status, actualTxsStatus)

	txProcessedTotalMetric := env.validatorCommitterManager.config.metrics.vcserviceTransactionProcessedTotal
	txTotal := test.GetIntMetricValue(t, txProcessedTotalMetric)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	env.mockVcServices[0].SubmitTransactions(ctx, &protovcservice.TransactionBatch{
		Transactions: []*protovcservice.Transaction{
			{ID: "untrackedTxID1", BlockNumber: 1, TxNum: 1},
			{ID: "untrackedTxID2", BlockNumber: 2, TxNum: 2},
		},
	})
	require.Never(t, func() bool {
		return test.GetIntMetricValue(t, txProcessedTotalMetric) > txTotal
	}, 2*time.Second, 1*time.Second)
}

func createInputTxsNodeForTest(numTxs, startIndex int, blkNum uint64) (
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
