package coordinator

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/fabricx-config/internaltools/configtxgen"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/coordinator/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
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
			metrics:                        newPerformanceMetrics(),
			policyMgr:                      svEnv.policyManager,
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
	label := sv.conn.CanonicalTarget()
	connStatus, err := sv.metrics.vcservicesConnectionStatus.GetMetricWithLabelValues(label)
	require.NoError(t, err)

	connFailure, err := sv.metrics.vcservicesConnectionFailureTotal.GetMetricWithLabelValues(label)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, connStatus) == float64(expectedConnStatus) &&
			test.GetMetricValue(t, connFailure) == float64(expectedConnFailureTotal)
	}, 30*time.Second, 200*time.Millisecond)
}

func (e *vcMgrTestEnv) requireRetriedTxsTotal(t *testing.T, expectedRetridTxsTotal int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, e.validatorCommitterManager.config.metrics.vcservicesRetriedTransactionTotal) ==
			float64(expectedRetridTxsTotal)
	}, 5*time.Second, 250*time.Millisecond)
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

		configBlockPath := config.CreateConfigBlock(t, &config.ConfigBlock{
			MetaNamespaceVerificationKey: verificationKey,
		})
		configBlock, err := configtxgen.ReadBlock(configBlockPath)
		require.NoError(t, err)

		txBatch := dependencygraph.TxNodeBatch{
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
