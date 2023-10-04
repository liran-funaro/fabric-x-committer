package dependencygraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/workerpool"
)

func TestGlobalDependencyManager(t *testing.T) {
	t.Parallel()

	incomingTxs := make(chan *transactionNodeBatch, 10)
	outgoingTxs := make(chan []*TransactionNode, 10)
	validatedTxs := make(chan []*TransactionNode, 10)
	workerConfig := &workerpool.Config{
		Parallelism:     10,
		ChannelCapacity: 20,
	}
	metrics := newPerformanceMetrics(true, prometheusmetrics.NewProvider())
	dm := newGlobalDependencyManager(
		&globalDepConfig{
			incomingTxsNode:        incomingTxs,
			outgoingDepFreeTxsNode: outgoingTxs,
			validatedTxsNode:       validatedTxs,
			workerPoolConfig:       workerConfig,
			waitingTxsLimit:        10,
			metrics:                metrics,
		},
	)
	dm.start()

	keys := makeTestKeys(t, 10)

	t.Run("dependency free txs", func(t *testing.T) {
		noDepsTxs := []*TransactionNode{
			createTxNode(t, [][]byte{keys[0]}, [][]byte{keys[1]}, [][]byte{keys[2]}),
			createTxNode(t, [][]byte{keys[3]}, [][]byte{keys[4]}, [][]byte{keys[5]}),
			createTxNode(t, [][]byte{keys[6]}, [][]byte{keys[7]}, [][]byte{keys[8]}),
		}

		incomingTxs <- createTxsNodeBatch(t, noDepsTxs)
		depFreeTxs := <-outgoingTxs
		// as there are no dependencies, all three transactions should be returned
		require.Equal(t, noDepsTxs, depFreeTxs)
		validatedTxs <- depFreeTxs
		// once we validate the transactions, the dependency manager should remove
		// the reads and writes performed by these transactions from the dependency detector
		// so that the new transaction won't get a wrong dependency.
		ensureEmptyDetector(t, dm.dependencyDetector)
		require.Eventually(t, func() bool {
			return test.GetMetricValue(t, metrics.gdgTxProcessedTotal) == 3 &&
				test.GetMetricValue(t, metrics.gdgValidatedTxProcessedTotal) == 3
		}, 2*time.Second, 200*time.Millisecond)
	})

	t.Run("local dependency but no global dependency", func(t *testing.T) {
		// t3 depends on t2 depends on t1
		t1 := createTxNode(t, [][]byte{keys[0]}, [][]byte{keys[1]}, nil)
		t2 := createTxNode(t, [][]byte{keys[1]}, [][]byte{keys[2]}, nil)
		t3 := createTxNode(t, [][]byte{keys[2]}, [][]byte{keys[3]}, nil)

		t2.dependsOnTxs = append(t2.dependsOnTxs, t1)
		t1.dependentTxs.Store(t2, struct{}{})

		t3.dependsOnTxs = append(t3.dependsOnTxs, t2)
		t2.dependentTxs.Store(t3, struct{}{})

		incomingTxs <- createTxsNodeBatch(t, []*TransactionNode{t1, t2, t3})
		depFreeTxs := <-outgoingTxs
		// only dependency free tx is t1
		require.Equal(t, []*TransactionNode{t1}, depFreeTxs)

		require.Equal(t, float64(3), test.GetMetricValue(t, metrics.gdgWaitingTxQueueSize))

		validatedTxs <- []*TransactionNode{t1}

		require.Eventually(t, func() bool {
			return test.GetMetricValue(t, metrics.gdgWaitingTxQueueSize) == 2
		}, 2*time.Second, 200*time.Millisecond)

		depFreeTxs = <-outgoingTxs
		// after validating t1, t2 becomes dependency free
		require.Equal(t, []*TransactionNode{t2}, depFreeTxs)
		require.Len(t, t2.dependsOnTxs, 0)

		validatedTxs <- []*TransactionNode{t2}
		depFreeTxs = <-outgoingTxs
		// after validating t2, t3 becomes dependency free
		require.Equal(t, []*TransactionNode{t3}, depFreeTxs)
		require.Len(t, t3.dependsOnTxs, 0)

		validatedTxs <- []*TransactionNode{t3}
		// after validating t3, there is no more txs
		ensureEmptyDetector(t, dm.dependencyDetector)

		// as we are not updating the dependentsTxs of validated txs,
		// the number of dependents is non-zero for t1.
		require.Equal(t, 1, getLengthOfDependentTx(t, t1.dependentTxs))
	})

	t.Run("both local and global dependency", func(t *testing.T) {
		// t2 depends on t1
		t1 := createTxNode(t, [][]byte{keys[0], keys[1]}, [][]byte{keys[2], keys[3]}, [][]byte{keys[4], keys[5]})
		t2 := createTxNode(t, [][]byte{keys[4], keys[5]}, [][]byte{keys[2], keys[6]}, [][]byte{keys[3], keys[7]})

		t2.dependsOnTxs = append(t2.dependsOnTxs, t1)
		t1.dependentTxs.Store(t2, struct{}{})

		incomingTxs <- createTxsNodeBatch(t, []*TransactionNode{t1, t2})

		// t3 depends on t2 and t1
		t3 := createTxNode(t, [][]byte{keys[7], keys[3]}, [][]byte{keys[2], keys[3]}, [][]byte{keys[8], keys[5]})
		// t4 depends on t2 and t1
		t4 := createTxNode(t, [][]byte{keys[7], keys[6]}, [][]byte{keys[4], keys[1]}, [][]byte{keys[0], keys[9]})

		incomingTxs <- createTxsNodeBatch(t, []*TransactionNode{t3, t4})

		// only t1 is dependency free
		depFreeTxs := <-outgoingTxs
		require.Equal(t, []*TransactionNode{t1}, depFreeTxs)

		// t1 has three dependents: t2, t3, and t4
		require.Eventually(t, func() bool {
			return getLengthOfDependentTx(t, t1.dependentTxs) == 3
		}, 2*time.Second, 200*time.Millisecond)
		for _, txNode := range []*TransactionNode{t2, t3, t4} {
			_, exist := t1.dependentTxs.Load(txNode)
			require.True(t, exist)
		}

		validatedTxs <- []*TransactionNode{t1}

		// after validating t1, t2 becomes dependency free
		depFreeTxs = <-outgoingTxs
		require.Equal(t, []*TransactionNode{t2}, depFreeTxs)

		// t2 has two dependents: t3 and t4
		require.Equal(t, 2, getLengthOfDependentTx(t, t2.dependentTxs))
		for _, txNode := range []*TransactionNode{t3, t4} {
			_, exist := t2.dependentTxs.Load(txNode)
			require.True(t, exist)
		}

		validatedTxs <- []*TransactionNode{t2}

		// after validating t2, both t3 and t4 become dependency free
		depFreeTxs = <-outgoingTxs
		require.Len(t, depFreeTxs, 2)
		require.ElementsMatch(t, []*TransactionNode{t3, t4}, depFreeTxs)

		validatedTxs <- []*TransactionNode{t3, t4}

		// after validating t3 and t4, there is no more txs
		ensureEmptyDetector(t, dm.dependencyDetector)
	})

	t.Run("both local and global dependency with waiting due to limit", func(t *testing.T) {
		dm.waitingTxsSlots.availableSlots.Store(2)
		// t2 depends on t1
		t1 := createTxNode(t, [][]byte{keys[0], keys[1]}, [][]byte{keys[2], keys[3]}, [][]byte{keys[4], keys[5]})
		t2 := createTxNode(t, [][]byte{keys[4], keys[5]}, [][]byte{keys[2], keys[6]}, [][]byte{keys[3], keys[7]})

		t2.dependsOnTxs = append(t2.dependsOnTxs, t1)
		t1.dependentTxs.Store(t2, struct{}{})

		incomingTxs <- createTxsNodeBatch(t, []*TransactionNode{t1, t2})

		// t3 depends on t2 and t1
		t3 := createTxNode(t, [][]byte{keys[7], keys[3]}, [][]byte{keys[2], keys[3]}, [][]byte{keys[8], keys[5]})

		incomingTxs <- createTxsNodeBatch(t, []*TransactionNode{t3})

		// only t1 is dependency free
		depFreeTxs := <-outgoingTxs
		require.Equal(t, []*TransactionNode{t1}, depFreeTxs)

		// t1 has two dependents: t2, and t3 but t2 is waiting due to the limit and not processed yet.
		// Hence, t1 should have only one dependent which is t2.
		require.Eventually(t, func() bool {
			return getLengthOfDependentTx(t, t1.dependentTxs) == 1
		}, 2*time.Second, 200*time.Millisecond)
		_, exist := t1.dependentTxs.Load(t2)
		require.True(t, exist)

		validatedTxs <- []*TransactionNode{t1}

		// after validating t1, t2 becomes dependency free
		depFreeTxs = <-outgoingTxs
		require.Equal(t, []*TransactionNode{t2}, depFreeTxs)

		// t2 has one dependent: t3. As t3 was waiting due to the limit, it might not have been added to
		// the dependency graph yet. However, now, t3 should not be waiting given t1 is removed. Hence,
		// we need to wait until t3 is added to the dependency graph.
		require.Eventually(t, func() bool {
			return getLengthOfDependentTx(t, t2.dependentTxs) == 1
		}, 2*time.Second, 200*time.Millisecond)

		validatedTxs <- []*TransactionNode{t2}

		// after validating t2, t2 becomes dependency free
		// as t1 and t2 are already validated and removed.
		depFreeTxs = <-outgoingTxs
		require.Len(t, depFreeTxs, 1)
		require.ElementsMatch(t, []*TransactionNode{t3}, depFreeTxs)

		validatedTxs <- []*TransactionNode{t3}

		// after validating t3, there is no more txs
		ensureEmptyDetector(t, dm.dependencyDetector)
	})
}

func createTxsNodeBatch(_ *testing.T, txsNode []*TransactionNode) *transactionNodeBatch {
	localDepDetect := newDependencyDetector()
	for _, tx := range txsNode {
		localDepDetect.addWaitingTx(tx)
	}

	return &transactionNodeBatch{
		txsNode:          txsNode,
		localDepDetector: localDepDetect,
	}
}

func ensureEmptyDetector(t *testing.T, d *dependencyDetector) {
	require.Eventually(t, func() bool {
		return len(d.readKeyToWaitingTxs) == 0 && len(d.writeKeyToWaitingTxs) == 0
	}, 2*time.Second, 100*time.Millisecond)
}
