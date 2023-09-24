package dependencygraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type localDependencyConstructorTestEnv struct {
	inComingTxs chan *TransactionBatch
	outGoingTxs chan *transactionNodeBatch
	metrics     *perfMetrics
}

func newLocalDependencyConstructorTestEnv(t *testing.T) *localDependencyConstructorTestEnv {
	inComingTxs := make(chan *TransactionBatch, 5)
	outGoingTxs := make(chan *transactionNodeBatch, 5)

	metrics := newPerformanceMetrics(true, prometheusmetrics.NewProvider())
	newLocalDependencyConstructor(inComingTxs, outGoingTxs, metrics).start(5)

	t.Cleanup(func() {
		close(inComingTxs)
		close(outGoingTxs)
	})

	return &localDependencyConstructorTestEnv{
		inComingTxs: inComingTxs,
		outGoingTxs: outGoingTxs,
		metrics:     metrics,
	}
}

func TestLocalDependencyConstructorWithDependencies(t *testing.T) {
	t.Parallel()

	keys := makeTestKeys(t, 24)

	env := newLocalDependencyConstructorTestEnv(t)

	t.Run("no dependencies between transactions", func(t *testing.T) {
		noDepsTxs := &TransactionBatch{
			ID: 1,
			Txs: []*protoblocktx.Tx{
				createTxForTest(
					t,
					[][]byte{keys[0], keys[1]},
					[][]byte{keys[2], keys[3]},
					[][]byte{keys[4], keys[5]},
				),
				createTxForTest(
					t,
					[][]byte{keys[6], keys[7]},
					[][]byte{keys[8], keys[9]},
					[][]byte{keys[10], keys[11]},
				),
				createTxForTest(
					t,
					[][]byte{keys[12], keys[13]},
					[][]byte{keys[14], keys[15]},
					[][]byte{keys[16], keys[17]},
				),
				createTxForTest(
					t,
					[][]byte{keys[18], keys[19]},
					[][]byte{keys[20], keys[21]},
					[][]byte{keys[22], keys[23]},
				),
			},
		}
		env.inComingTxs <- noDepsTxs

		txsNodeBatch := <-env.outGoingTxs
		txsNode := txsNodeBatch.txsNode
		require.Len(t, txsNode, 4)
		for _, txNode := range txsNode {
			// as there are no dependencies, both the dependsOnTxs and dependents list should be empty
			require.Len(t, txNode.dependsOnTxs, 0)
			require.Equal(t, 0, getLengthOfDependentTx(t, txNode.dependentTxs))
		}

		require.Eventually(t, func() bool {
			return test.GetMetricValue(t, env.metrics.ldgTxProcessedTotal) == 4
		}, 2*time.Second, 200*time.Millisecond)
	})

	t.Run("linear dependency i and i+1 transaction", func(t *testing.T) {
		noDepsTxs := &TransactionBatch{
			ID: 2,
			Txs: []*protoblocktx.Tx{
				createTxForTest(t, [][]byte{keys[1]}, [][]byte{keys[2]}, nil),
				createTxForTest(t, [][]byte{keys[2]}, [][]byte{keys[3]}, nil),
				createTxForTest(t, [][]byte{keys[3]}, [][]byte{keys[4]}, nil),
				createTxForTest(t, [][]byte{keys[4]}, [][]byte{keys[5]}, nil),
			},
		}
		env.inComingTxs <- noDepsTxs

		txsNodeBatch := <-env.outGoingTxs
		txsNode := txsNodeBatch.txsNode
		require.Len(t, txsNode, 4)
		for i, txNode := range txsNode {
			switch i {
			case 0:
				// tx1 should not have any dependencies
				require.Len(t, txNode.dependsOnTxs, 0)
			default:
				// other transactions should be dependent on the previous transaction
				require.Len(t, txNode.dependsOnTxs, 1)
				require.Equal(t, transactionSet{txsNode[i-1]: nil}, txNode.dependsOnTxs)
			}

			if i == len(txsNode)-1 {
				// last transaction should not have any dependents
				require.Equal(t, 0, getLengthOfDependentTx(t, txNode.dependentTxs))
			} else {
				// other transactions should have the next transaction as dependent
				require.Equal(t, 1, getLengthOfDependentTx(t, txNode.dependentTxs))
				_, exist := txNode.dependentTxs.Load(txsNode[i+1])
				require.True(t, exist)
			}
		}
	})
}

func TestLocalDependencyConstructorWithOrder(t *testing.T) {
	t.Parallel()

	env := newLocalDependencyConstructorTestEnv(t)

	keys := makeTestKeys(t, 9)

	// send the transactions in reverse order
	noDepsTxs := &TransactionBatch{
		ID: 3,
		Txs: []*protoblocktx.Tx{
			createTxForTest(t, nil, [][]byte{keys[4]}, nil),
			createTxForTest(t, nil, [][]byte{keys[5]}, nil),
			createTxForTest(t, nil, [][]byte{keys[6]}, nil),
			createTxForTest(t, nil, [][]byte{keys[7]}, nil),
			createTxForTest(t, nil, [][]byte{keys[8]}, nil),
		},
	}
	env.inComingTxs <- noDepsTxs

	noDepsTxs = &TransactionBatch{
		ID: 2,
		Txs: []*protoblocktx.Tx{
			createTxForTest(t, nil, [][]byte{keys[1]}, nil),
			createTxForTest(t, nil, [][]byte{keys[2]}, nil),
			createTxForTest(t, nil, [][]byte{keys[3]}, nil),
		},
	}
	env.inComingTxs <- noDepsTxs

	select {
	case txsNode := <-env.outGoingTxs:
		t.Fatal("should not have received txsNode: %w", txsNode)
	case <-time.After(1 * time.Second):
	}

	noDepsTxs = &TransactionBatch{
		ID: 1,
		Txs: []*protoblocktx.Tx{
			createTxForTest(t, nil, [][]byte{keys[0]}, nil),
		},
	}
	env.inComingTxs <- noDepsTxs

	// id 1, 2, 3 should be received in order though we have sent id 3, 2, 1 in the order
	tests := []struct {
		txsNodeBatch *transactionNodeBatch
		len          int
	}{
		{
			txsNodeBatch: <-env.outGoingTxs,
			len:          1,
		},
		{
			txsNodeBatch: <-env.outGoingTxs,
			len:          3,
		},
		{
			txsNodeBatch: <-env.outGoingTxs,
			len:          5,
		},
	}

	for _, test := range tests {
		txsNode := test.txsNodeBatch.txsNode
		require.Len(t, txsNode, test.len)
		for _, txNode := range txsNode {
			require.Len(t, txNode.dependsOnTxs, 0)
			require.Equal(t, 0, getLengthOfDependentTx(t, txNode.dependentTxs))
		}
	}
}

func makeTestKeys(_ *testing.T, numKeys int) [][]byte {
	keys := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = []byte{byte(i)}
	}
	return keys
}
