package coordinatorservice

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type vcMgrTestEnv struct {
	validatorCommitterManager *validatorCommitterManager
	inputTxs                  chan []*dependencygraph.TransactionNode
	outputTxs                 chan []*dependencygraph.TransactionNode
	outputTxsStatus           chan *protovcservice.TransactionStatus
	prelimInvalidTxStatus     chan []*protovcservice.Transaction
	mockVcServices            []*mock.VcService
}

func newVcMgrTestEnv(t *testing.T, numVCService int) *vcMgrTestEnv {
	vcs, servers := mock.StartMockVCService(t, numVCService)

	inputTxs := make(chan []*dependencygraph.TransactionNode, 10)
	outputTxs := make(chan []*dependencygraph.TransactionNode, 10)
	outputTxsStatus := make(chan *protovcservice.TransactionStatus, 10)
	prelimInvalidTxStatus := make(chan []*protovcservice.Transaction, 10)

	vcm := newValidatorCommitterManager(
		&validatorCommitterManagerConfig{
			serversConfig:                           servers.Configs,
			incomingTxsForValidationCommit:          inputTxs,
			outgoingValidatedTxsNode:                outputTxs,
			outgoingTxsStatus:                       outputTxsStatus,
			incomingPrelimInvalidTxsStatusForCommit: prelimInvalidTxStatus,
			metrics:                                 newPerformanceMetrics(true),
		},
	)

	test.RunServiceForTest(t, func(ctx context.Context) error {
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
				txsStatus2 *protovcservice.TransactionStatus,
			) *protovcservice.TransactionStatus {
				txsStatus := &protovcservice.TransactionStatus{
					Status: make(map[string]protoblocktx.Status),
				}
				for id, status := range txsStatus1.Status {
					txsStatus.Status[id] = status
				}
				for id, status := range txsStatus2.Status {
					txsStatus.Status[id] = status
				}

				return txsStatus
			}

			require.Equal(
				t,
				mergeTxsStatus(expectedTxsStatus1, expectedTxsStatus2).Status,
				mergeTxsStatus(outTxsStatus1, outTxsStatus2).Status,
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

		require.Equal(t, map[string]protoblocktx.Status{
			"1": protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED,
			"2": protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID,
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

	for _, vc := range env.validatorCommitterManager.validatorCommitter {
		count := 0
		vc.txBeingValidated.Range(func(_, _ any) bool {
			count++
			return true
		})
		require.Zero(t, count)
	}
}

func createInputTxsNodeForTest(_ *testing.T, numTxs, startIndex, blkNum int) (
	[]*dependencygraph.TransactionNode, *protovcservice.TransactionStatus,
) {
	txsNode := make([]*dependencygraph.TransactionNode, numTxs)
	expectedTxsStatus := &protovcservice.TransactionStatus{
		Status: make(map[string]protoblocktx.Status),
	}

	for i := 0; i < numTxs; i++ {
		id := "tx" + strconv.Itoa(startIndex+i)
		txsNode[i] = &dependencygraph.TransactionNode{
			Tx: &protovcservice.Transaction{
				ID:          id,
				BlockNumber: uint64(blkNum),
				TxNum:       uint32(i), //nolint:gosec
			},
		}
		expectedTxsStatus.Status[id] = protoblocktx.Status_COMMITTED
	}

	return txsNode, expectedTxsStatus
}
