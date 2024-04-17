package dependencygraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/workerpool"
)

func TestDependencyGraph(t *testing.T) {
	t.Parallel()

	localDepIncomingTxs := make(chan *TransactionBatch, 10)
	localDepOutgoingTxs := make(chan *transactionNodeBatch, 10)

	metrics := newPerformanceMetrics(true, prometheusmetrics.NewProvider())
	ldc := newLocalDependencyConstructor(localDepIncomingTxs, localDepOutgoingTxs, metrics)
	ldc.start(2)

	globalDepOutgoingTxs := make(chan []*TransactionNode, 10)
	validatedTxs := make(chan []*TransactionNode, 10)
	workerConfig := &workerpool.Config{
		Parallelism:     10,
		ChannelCapacity: 20,
	}
	dm := newGlobalDependencyManager(
		&globalDepConfig{
			incomingTxsNode:        localDepOutgoingTxs,
			outgoingDepFreeTxsNode: globalDepOutgoingTxs,
			validatedTxsNode:       validatedTxs,
			workerPoolConfig:       workerConfig,
			waitingTxsLimit:        20,
			metrics:                metrics,
		},
	)
	dm.start()

	t.Cleanup(func() {
		close(localDepIncomingTxs)
		close(localDepOutgoingTxs)
		close(globalDepOutgoingTxs)
		close(validatedTxs)
	})

	t.Run("check reads and writes dependency tracking", func(t *testing.T) {
		keys := makeTestKeys(t, 10)

		// t2 depends on t1
		t1 := createTxForTest(
			t, nsID1ForTest, [][]byte{keys[0], keys[1]}, [][]byte{keys[2], keys[3]}, [][]byte{keys[4], keys[5]},
		)
		t2 := createTxForTest(
			t, nsID1ForTest, [][]byte{keys[4], keys[5]}, [][]byte{keys[2], keys[6]}, [][]byte{keys[3], keys[7]},
		)

		localDepIncomingTxs <- &TransactionBatch{
			ID:  1,
			Txs: []*protoblocktx.Tx{t1, t2},
		}

		// t3 depends on t2 and t1
		t3 := createTxForTest(
			t, nsID1ForTest, [][]byte{keys[7], keys[3]}, [][]byte{keys[2], keys[3]}, [][]byte{keys[8], keys[5]},
		)
		// t4 depends on t2 and t1
		t4 := createTxForTest(
			t, nsID1ForTest, [][]byte{keys[7], keys[6]}, [][]byte{keys[4], keys[1]}, [][]byte{keys[0], keys[9]},
		)

		localDepIncomingTxs <- &TransactionBatch{
			ID:  2,
			Txs: []*protoblocktx.Tx{t3, t4},
		}

		// only t1 is dependency free
		depFreeTxs := <-globalDepOutgoingTxs
		require.Len(t, depFreeTxs, 1)
		actualT1 := depFreeTxs[0]
		require.Equal(t, t1.Id, actualT1.Tx.ID)

		// t1 has 3 dependent transactions, t2, t3, and t4
		require.Eventually(t, func() bool {
			return getLengthOfDependentTx(t, actualT1.dependentTxs) == 3
		}, 2*time.Second, 200*time.Millisecond)

		validatedTxs <- []*TransactionNode{actualT1}

		// after t1 is validated, t2 is dependency free
		depFreeTxs = <-globalDepOutgoingTxs
		require.Len(t, depFreeTxs, 1)
		actualT2 := depFreeTxs[0]
		require.Equal(t, t2.Id, actualT2.Tx.ID)

		// t2 has 2 dependent transactions, t3 and t4
		require.Equal(t, 2, getLengthOfDependentTx(t, actualT2.dependentTxs))

		validatedTxs <- []*TransactionNode{actualT2}

		// after t2 is validated, both t3 and t4 are dependency free
		depFreeTxs = <-globalDepOutgoingTxs
		require.Len(t, depFreeTxs, 2)
		var actualT3, actualT4 *TransactionNode
		if t3.Id == depFreeTxs[0].Tx.ID {
			actualT3 = depFreeTxs[0]
			actualT4 = depFreeTxs[1]
		} else {
			actualT3 = depFreeTxs[1]
			actualT4 = depFreeTxs[0]
		}
		require.Equal(t, t3.Id, actualT3.Tx.ID)
		require.Equal(t, t4.Id, actualT4.Tx.ID)

		validatedTxs <- []*TransactionNode{actualT3, actualT4}

		ensureProcessedAndValidatedMetrics(t, metrics, 4, 4)
		// after validating all txs, the dependency detector should be empty
		ensureEmptyDetector(t, dm.dependencyDetector)
	})

	t.Run("check dependency in namespace", func(t *testing.T) {
		keys := makeTestKeys(t, 10)
		// t2 depends on t1
		t1 := createTxForTest(
			t, uint32(types.MetaNamespaceID), nil, [][]byte{types.NamespaceID(nsID1ForTest).Bytes()}, nil,
		)
		t2 := createTxForTest(
			t, nsID1ForTest, [][]byte{keys[4], keys[5]}, [][]byte{keys[2], keys[6]}, [][]byte{keys[3], keys[7]},
		)

		localDepIncomingTxs <- &TransactionBatch{
			ID:  3,
			Txs: []*protoblocktx.Tx{t1, t2},
		}

		// t3 depends on t2 and t1
		t3 := createTxForTest(
			t, uint32(types.MetaNamespaceID), nil, [][]byte{types.NamespaceID(nsID1ForTest).Bytes()}, nil,
		)
		// t4 depends on t3, t2 and t1
		t4 := createTxForTest(
			t, nsID1ForTest, [][]byte{keys[7], keys[6]}, [][]byte{keys[4], keys[1]}, [][]byte{keys[0], keys[9]},
		)

		localDepIncomingTxs <- &TransactionBatch{
			ID:  4,
			Txs: []*protoblocktx.Tx{t3, t4},
		}

		// only t1 is dependency free
		depFreeTxs := <-globalDepOutgoingTxs
		require.Len(t, depFreeTxs, 1)
		actualT1 := depFreeTxs[0]
		require.Equal(t, t1.Id, actualT1.Tx.ID)

		// t1 has 3 dependent transactions, t2, t3, and t4
		require.Eventually(t, func() bool {
			return getLengthOfDependentTx(t, actualT1.dependentTxs) == 3
		}, 2*time.Second, 200*time.Millisecond)

		validatedTxs <- []*TransactionNode{actualT1}

		// after t1 is validated, t2 is dependency free
		depFreeTxs = <-globalDepOutgoingTxs
		require.Len(t, depFreeTxs, 1)
		actualT2 := depFreeTxs[0]
		require.Equal(t, t2.Id, actualT2.Tx.ID)

		// t2 has 2 dependent transactions, t3 and t4
		require.Equal(t, 2, getLengthOfDependentTx(t, actualT2.dependentTxs))

		validatedTxs <- []*TransactionNode{actualT2}

		// after t2 is validated, t3 becomes dependency free
		depFreeTxs = <-globalDepOutgoingTxs
		require.Len(t, depFreeTxs, 1)
		actualT3 := depFreeTxs[0]
		require.Equal(t, t3.Id, actualT3.Tx.ID)

		validatedTxs <- []*TransactionNode{actualT3}

		// after t3 is validated, t4 becomes dependency free
		depFreeTxs = <-globalDepOutgoingTxs
		require.Len(t, depFreeTxs, 1)
		actualT4 := depFreeTxs[0]
		require.Equal(t, t4.Id, actualT4.Tx.ID)

		validatedTxs <- []*TransactionNode{actualT4}

		ensureProcessedAndValidatedMetrics(t, metrics, 8, 8)
		// after validating all txs, the dependency detector should be empty
		ensureEmptyDetector(t, dm.dependencyDetector)
	})
}
