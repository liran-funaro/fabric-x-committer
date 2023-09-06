package dependencygraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/workerpool"
)

func TestDependencyGraph(t *testing.T) {
	localDepIncomingTxs := make(chan *transactionBatch, 10)
	localDepOutgoingTxs := make(chan *transactionNodeBatch, 10)
	ldc := newLocalDependencyConstructor(localDepIncomingTxs, localDepOutgoingTxs)
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
		},
	)
	dm.start()

	t.Cleanup(func() {
		close(localDepIncomingTxs)
		close(localDepOutgoingTxs)
		close(globalDepOutgoingTxs)
		close(validatedTxs)
	})

	keys := makeTestKeys(t, 10)

	// t2 depends on t1
	t1 := createTxWithTxID(t, [][]byte{keys[0], keys[1]}, [][]byte{keys[2], keys[3]}, [][]byte{keys[4], keys[5]})
	t2 := createTxWithTxID(t, [][]byte{keys[4], keys[5]}, [][]byte{keys[2], keys[6]}, [][]byte{keys[3], keys[7]})

	localDepIncomingTxs <- &transactionBatch{
		id:          1,
		txsWithTxID: []*transactionWithTxID{t1, t2},
	}

	// t3 depends on t2 and t1
	t3 := createTxWithTxID(t, [][]byte{keys[7], keys[3]}, [][]byte{keys[2], keys[3]}, [][]byte{keys[8], keys[5]})
	// t4 depends on t2 and t1
	t4 := createTxWithTxID(t, [][]byte{keys[7], keys[6]}, [][]byte{keys[4], keys[1]}, [][]byte{keys[0], keys[9]})

	localDepIncomingTxs <- &transactionBatch{
		id:          2,
		txsWithTxID: []*transactionWithTxID{t3, t4},
	}

	// only t1 is dependency free
	depFreeTxs := <-globalDepOutgoingTxs
	require.Len(t, depFreeTxs, 1)
	actualT1 := depFreeTxs[0]
	require.Equal(t, t1.txID, actualT1.Tx.ID)

	// t1 has 3 dependent transactions, t2, t3, and t4
	require.Eventually(t, func() bool {
		return getLengthOfDependentTx(t, actualT1.dependentTxs) == 3
	}, 2*time.Second, 200*time.Millisecond)

	validatedTxs <- []*TransactionNode{actualT1}

	// after t1 is validated, t2 is dependency free
	depFreeTxs = <-globalDepOutgoingTxs
	require.Len(t, depFreeTxs, 1)
	actualT2 := depFreeTxs[0]
	require.Equal(t, t2.txID, actualT2.Tx.ID)

	// t2 has 2 dependent transactions, t3 and t4
	require.Equal(t, 2, getLengthOfDependentTx(t, actualT2.dependentTxs))

	validatedTxs <- []*TransactionNode{actualT2}

	// after t2 is validated, both t3 and t4 are dependency free
	depFreeTxs = <-globalDepOutgoingTxs
	require.Len(t, depFreeTxs, 2)
	var actualT3, actualT4 *TransactionNode
	if t3.txID == depFreeTxs[0].Tx.ID {
		actualT3 = depFreeTxs[0]
		actualT4 = depFreeTxs[1]
	} else {
		actualT3 = depFreeTxs[1]
		actualT4 = depFreeTxs[0]
	}
	require.Equal(t, t3.txID, actualT3.Tx.ID)
	require.Equal(t, t4.txID, actualT4.Tx.ID)

	validatedTxs <- []*TransactionNode{actualT3, actualT4}

	// after validating all txs, the dependency detector should be empty
	ensureEmptyDetector(t, dm.dependencyDetector)
}
