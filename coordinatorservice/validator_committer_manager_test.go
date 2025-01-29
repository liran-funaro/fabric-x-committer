package coordinatorservice

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/protobuf/proto"
)

type vcMgrTestEnv struct {
	validatorCommitterManager *validatorCommitterManager
	inputTxs                  chan dependencygraph.TxNodeBatch
	outputTxs                 chan dependencygraph.TxNodeBatch
	outputTxsStatus           chan *protoblocktx.TransactionsStatus
	prelimInvalidTxStatus     chan []*protovcservice.Transaction
	mockVcServices            []*mock.VcService
	sigVerTestEnv             *svMgrTestEnv
}

func newVcMgrTestEnv(t *testing.T, numVCService int) *vcMgrTestEnv {
	vcs, servers := mock.StartMockVCService(t, numVCService)
	svEnv := newSvMgrTestEnv(t, 2)

	inputTxs := make(chan dependencygraph.TxNodeBatch, 10)
	outputTxs := make(chan dependencygraph.TxNodeBatch, 10)
	outputTxsStatus := make(chan *protoblocktx.TransactionsStatus, 10)
	prelimInvalidTxStatus := make(chan []*protovcservice.Transaction, 10)

	vcm := newValidatorCommitterManager(
		&validatorCommitterManagerConfig{
			serversConfig:                         servers.Configs,
			incomingTxsForValidationCommit:        inputTxs,
			outgoingValidatedTxsNode:              outputTxs,
			outgoingTxsStatus:                     outputTxsStatus,
			incomingBadlyFormedTxsStatusForCommit: prelimInvalidTxStatus,
			metrics:                               newPerformanceMetrics(true),
			signVerifierMgr:                       svEnv.signVerifierManager,
		},
	)

	test.RunServiceForTest(context.Background(), t, func(ctx context.Context) error {
		return connection.FilterStreamErrors(vcm.run(ctx))
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
		prelimInvalidTxStatus:     prelimInvalidTxStatus,
		mockVcServices:            vcs,
		sigVerTestEnv:             svEnv,
	}
}

func TestValidatorCommitterManager(t *testing.T) {
	env := newVcMgrTestEnv(t, 2)

	t.Run("Send tx batch to use any vcservice", func(t *testing.T) {
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
	})

	t.Run("send batches to ensure all vcservices are used", func(t *testing.T) {
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
	})

	t.Run("send transactions with preliminary invalid status", func(t *testing.T) {
		txBatch := []*protovcservice.Transaction{
			{
				ID: "1",
				PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
					Code: protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED,
				},
				BlockNumber: 15,
				TxNum:       0,
			},
			{
				ID: "2",
				PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
					Code: protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID,
				},
				BlockNumber: 4,
				TxNum:       1,
			},
		}
		env.prelimInvalidTxStatus <- txBatch

		outTxsStatus := <-env.outputTxsStatus

		require.Equal(t, map[string]*protoblocktx.StatusWithHeight{
			"1": types.CreateStatusWithHeight(protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED, 15, 0),
			"2": types.CreateStatusWithHeight(protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID, 4, 1),
		}, outTxsStatus.Status)

		maxNum := uint64(0)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		t.Cleanup(cancel)
		for _, vc := range env.mockVcServices {
			// NOTE: As we are using mock service, the ledger data is not shared. Hence,
			//       we have to query each mock vcservice to find the last seen maximum
			//       block number.
			blk, err := vc.GetMaxSeenBlockNumber(ctx, nil)
			require.NoError(t, err)
			maxNum = max(maxNum, blk.Number)
		}
		require.Equal(t, uint64(15), maxNum)
	})

	t.Run("namespace transaction should update signature verifier", func(t *testing.T) {
		for _, mockSvService := range env.sigVerTestEnv.mockSvService {
			require.Nil(t, mockSvService.GetVerificationKey())
		}

		p := &protoblocktx.NamespacePolicy{
			Scheme:    "ECDSA",
			PublicKey: []byte("publicKey"),
		}
		pBytes, err := proto.Marshal(p)
		require.NoError(t, err)

		txBatch := dependencygraph.TxNodeBatch{
			{
				Tx: &protovcservice.Transaction{
					ID: "create ns 1",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId: uint32(types.MetaNamespaceID),
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key:   types.NamespaceID(1).Bytes(),
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
			require.Equal(t, []byte("publicKey"), mockSvService.GetVerificationKey())
		}
	})

	for _, vc := range env.validatorCommitterManager.validatorCommitter {
		count := 0
		vc.txBeingValidated.Range(func(_, _ any) bool {
			count++
			return true
		})
		require.Zero(t, count)
	}
}

func createInputTxsNodeForTest(_ *testing.T, numTxs, startIndex int, blkNum uint64) (
	[]*dependencygraph.TransactionNode, *protoblocktx.TransactionsStatus,
) {
	txsNode := make([]*dependencygraph.TransactionNode, numTxs)
	expectedTxsStatus := &protoblocktx.TransactionsStatus{
		Status: make(map[string]*protoblocktx.StatusWithHeight),
	}

	for i := 0; i < numTxs; i++ {
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
