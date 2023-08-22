package dependencygraph

import (
	"encoding/binary"
	"sync"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

type (
	transactionNode struct {
		txID string
		tx   *protoblocktx.Tx
		// dependsOnTxs is a set of transactions that this transaction depends on.
		// A transaction is eligible for validation once all the transactions
		// in dependsOnTxs set are validated.
		// Only after validating transactions present in dependsOnTxs set,
		// this transaction can be considered for validation.
		// Though we can track finer-grained dependencies such as read-write,
		// write-read, and write-write, we only track the coarse-grained dependency
		// because of the following reasons:
		//   1. The coarse-grained dependency tracking certainly helps the dependency
		//      manager to have less complex logic and efficient implementation.
		//   2. Further, only for read-write dependency, we can invalidate the dependent
		//      if the dependency tx is valid. For other dependencies, we can validate
		//      the dependent irrespective of whether the dependency tx is valid or not.
		//      The write-read and write-write dependencies only help to serialize the
		//      transactions which can be achieved using this simple dependsOnTxs itself.
		//      If we can track finer-grained dependencies, using read-write dependency, we
		//      can invalidate certain transactions in advance thus saving some time.
		//      However, we do not expect the transactions to have many dependencies
		//      among them especially read-write dependencies.
		// If the need arises, we can track finer-grained dependencies.
		// Note that a transaction T2 can depend on another transaction T1 only if T1
		// has arrived before T2 or T1 precedes T2 in transaction order in the block.
		// Hence, we will not have cyclic dependencies.
		dependsOnTxs transactionSet
		// dependentTxs is a set of transactions that depend on this transaction.
		// After validating this transaction, dependentTxs is used to remove dependencies
		// from each dependent transaction.
		dependentTxs *sync.Map
		rwKeys       *readWriteKeys
	}

	// transactionSet is a map with keys as transactionNode. This is used to
	// facilitate efficient lookup of transactions.
	transactionSet map[*transactionNode]any

	// readWriteKeys holds the read and write keys of a transaction.
	readWriteKeys struct {
		reads  []string
		writes []string
	}
)

func newTransactionNode(txWithID *transactionWithTxID) *transactionNode {
	return &transactionNode{
		txID:         txWithID.txID,
		tx:           txWithID.tx,
		dependsOnTxs: make(transactionSet),
		dependentTxs: newDependentTxs(),
		rwKeys:       readAndWriteKeys(txWithID.tx.Namespaces),
	}
}

func newDependentTxs() *sync.Map {
	return &sync.Map{}
}

// addDependenciesAndUpdateDependents adds input transactions as dependencies
// to the transaction object on which this method is called. Further, it also
// updates the dependents of input transactions.
// Node-level concurrency:
// This method cannot be called concurrently on the same
// transaction node with freeDependents() and isDependencyFree().
// Graph-level concurrency:
// This method can be called concurrently on different transaction nodes.
// Though the dependsOnTx can be common among different transactions/goroutines,
// it is not a problem because dependentTxs is a concurrent map.
func (n *transactionNode) addDependenciesAndUpdateDependents(dependsOnTxs transactionSet) {
	if len(dependsOnTxs) == 0 {
		return
	}

	n.dependsOnTxs.update(dependsOnTxs)

	for depOnTx := range dependsOnTxs {
		depOnTx.dependentTxs.Store(n, nil)
	}
}

// freeDependents removes the transaction object (on which this method
// is called) as a dependency from all dependent transactions and returns
// the fully freed transactions, i.e., transactions with zero dependencies.
// Node-level concurrency:
// This method cannot be called concurrently on the same
// transaction node with addDependenciesAndUpdateDependents() and isDependencyFree().
// Graph-level concurrency:
// This method cannot be called concurrently on different transactions nodes too.
// This is because the concurrent execution of this method on different
// transaction nodes can modify the same dependsOnTxs set which is not protected
// by a lock.
func (n *transactionNode) freeDependents() []*transactionNode /* fully freed transactions */ {
	var freedTxs []*transactionNode

	n.dependentTxs.Range(func(k, _ any) bool {
		dependentTx, _ := k.(*transactionNode)

		delete(dependentTx.dependsOnTxs, n)

		if dependentTx.isDependencyFree() {
			freedTxs = append(freedTxs, dependentTx)
		}

		return true
	})

	return freedTxs
}

// isDependencyFree returns true if the transaction has no dependencies.
// Node-level concurrency:
// This method cannot be called concurrently on the same
// transaction node with addDependenciesAndUpdateDependents() and freeDependents().
// Graph-level concurrency:
// This method can be called concurrently on different transaction nodes.
func (n *transactionNode) isDependencyFree() bool {
	return len(n.dependsOnTxs) == 0
}

func (l transactionSet) update(tl transactionSet) {
	for t := range tl {
		l[t] = nil
	}
}

func readAndWriteKeys(txNamespaces []*protoblocktx.TxNamespace) *readWriteKeys {
	var readKeys []string
	var writeKeys []string

	for _, ns := range txNamespaces {
		for _, ro := range ns.ReadsOnly {
			readKeys = append(readKeys, constructCompositeKey(ns.NsId, ro.Key))
		}

		for _, rw := range ns.ReadWrites {
			k := constructCompositeKey(ns.NsId, rw.Key)
			readKeys = append(readKeys, k)
			writeKeys = append(writeKeys, k)
		}

		for _, w := range ns.BlindWrites {
			writeKeys = append(writeKeys, constructCompositeKey(ns.NsId, w.Key))
		}
	}

	return &readWriteKeys{
		reads:  readKeys,
		writes: writeKeys,
	}
}

func constructCompositeKey(ns uint32, key []byte) string {
	// NOTE: composite key construction must ensure
	//       no false positives collisions in the
	//       composite key space.
	ck := make([]byte, 0, len(key)+4)
	ck = binary.BigEndian.AppendUint32(ck, ns)
	ck = append(ck, key...)
	return string(ck)
}
