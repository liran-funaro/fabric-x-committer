package coordinatorservice

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type vcMgrTestEnv struct {
	validatorCommitterManager *validatorCommitterManager
	inputTxs                  chan []*dependencygraph.TransactionNode
	outputTxs                 chan []*dependencygraph.TransactionNode
	outputTxsStatus           chan *protovcservice.TransactionStatus
	mockVcServices            []*vcservicemock.MockVcService
}

func newVcMgrTestEnv(t *testing.T, numVCService int) *vcMgrTestEnv {
	sc, vcs, grpcSrvs := vcservicemock.StartMockVCService(numVCService)

	inputTxs := make(chan []*dependencygraph.TransactionNode, 10)
	outputTxs := make(chan []*dependencygraph.TransactionNode, 10)
	outputTxsStatus := make(chan *protovcservice.TransactionStatus, 10)

	ctx, cancelFunc := context.WithTimeout(context.Background(), 2*time.Minute)
	vcm := newValidatorCommitterManager(
		ctx,
		&validatorCommitterManagerConfig{
			serversConfig:                  sc,
			incomingTxsForValidationCommit: inputTxs,
			outgoingValidatedTxsNode:       outputTxs,
			outgoingTxsStatus:              outputTxsStatus,
			metrics:                        newPerformanceMetrics(true),
		},
	)
	errChan, err := vcm.start()
	t.Cleanup(cancelFunc)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		numErrorableVCMGoroutines := 3 * len(vcs)
		for i := 0; i < numErrorableVCMGoroutines; i++ {
			require.NoError(t, <-errChan)
		}
		wg.Done()
	}()

	t.Cleanup(func() {
		vcm.close()
		close(inputTxs)
		close(outputTxs)
		for _, mockVC := range vcs {
			mockVC.Close()
		}
		wg.Wait()
		close(errChan)
		for _, s := range grpcSrvs {
			s.Stop()
		}
	})

	return &vcMgrTestEnv{
		validatorCommitterManager: vcm,
		inputTxs:                  inputTxs,
		outputTxs:                 outputTxs,
		outputTxsStatus:           outputTxsStatus,
		mockVcServices:            vcs,
	}
}

func TestValidatorCommitterManager(t *testing.T) {
	env := newVcMgrTestEnv(t, 2)

	t.Run("Send tx batch to use any vcservice", func(t *testing.T) {
		txBatch, expectedTxsStatus := createInputTxsNodeForTest(t, 5, 1)
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
		txBatch1, expectedTxsStatus1 := createInputTxsNodeForTest(t, 5, 1)
		txBatch2, expectedTxsStatus2 := createInputTxsNodeForTest(t, 5, 6)

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
}

func createInputTxsNodeForTest(_ *testing.T, numTxs, startIndex int) (
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
				ID: id,
			},
		}
		expectedTxsStatus.Status[id] = protoblocktx.Status_COMMITTED
	}

	return txsNode, expectedTxsStatus
}
