/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package dependencygraph

import (
	"slices"
	"strings"

	"github.com/hyperledger/fabric-x-committer/api/protoblocktx"
	"github.com/hyperledger/fabric-x-committer/api/protovcservice"
	"github.com/hyperledger/fabric-x-committer/api/types"
	"github.com/hyperledger/fabric-x-committer/utils"
)

type (
	// TransactionNode is a node in the dependency graph.
	TransactionNode struct {
		Tx *protovcservice.Transaction
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
		dependsOnTxs TxNodeBatch
		// dependentTxs is a set of transactions that depend on this transaction.
		// After validating this transaction, dependentTxs is used to remove dependencies
		// from each dependent transaction.
		dependentTxs utils.SyncMap[*TransactionNode, any]
		rwKeys       *readWriteKeys
		Signatures   [][]byte

		// Used by the simple dependency graph.
		waitForKeysCount uint64
		waitingKeys      []*waiting
	}

	// TxNodeBatch is a batch of transaction nodes.
	TxNodeBatch []*TransactionNode

	// readWriteKeys holds the read and write keys of a transaction.
	readWriteKeys struct {
		readsOnly      []string
		writesOnly     []string
		readsAndWrites []string
	}
)

func newTransactionNode(blockNum uint64, txNum uint32, tx *protoblocktx.Tx) *TransactionNode {
	return &TransactionNode{
		Tx: &protovcservice.Transaction{
			ID:          tx.Id,
			Namespaces:  tx.Namespaces,
			BlockNumber: blockNum,
			TxNum:       txNum,
		},
		rwKeys:     readAndWriteKeys(tx.Namespaces),
		Signatures: tx.Signatures,
	}
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
func (n *TransactionNode) addDependenciesAndUpdateDependents(dependsOnTxs TxNodeBatch) {
	if len(dependsOnTxs) == 0 {
		return
	}

	n.dependsOnTxs = append(n.dependsOnTxs, dependsOnTxs...)

	for _, depOnTx := range dependsOnTxs {
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
func (n *TransactionNode) freeDependents() TxNodeBatch /* fully freed transactions */ {
	var freedTxs TxNodeBatch
	for dependentTx := range n.dependentTxs.IterKeys() {
		for i, tx := range dependentTx.dependsOnTxs {
			if tx == n {
				dependentTx.dependsOnTxs = slices.Delete(dependentTx.dependsOnTxs, i, i+1)
				break
			}
		}

		if dependentTx.isDependencyFree() {
			freedTxs = append(freedTxs, dependentTx)
		}
	}
	return freedTxs
}

// isDependencyFree returns true if the transaction has no dependencies.
// Node-level concurrency:
// This method cannot be called concurrently on the same
// transaction node with addDependenciesAndUpdateDependents() and freeDependents().
// Graph-level concurrency:
// This method can be called concurrently on different transaction nodes.
func (n *TransactionNode) isDependencyFree() bool {
	return len(n.dependsOnTxs) == 0
}

func readAndWriteKeys(txNamespaces []*protoblocktx.TxNamespace) *readWriteKeys {
	var readOnlyKeys, writeOnlyKeys, readAndWriteKeys []string //nolint:prealloc

	for _, ns := range txNamespaces {
		// To establish a clear dependency between namespace lifecycle transactions (involving creating,
		// updating, or deleting namespaces) and normal transactions (updating states within a namespace),
		// it's important to include the accessed namespace in the readOnlyKeys. For example, consider a
		// normal transaction writing to namespace ns1. If a subsequent transaction, a namespace lifecycle
		// transaction, changes ns1's policy, it should not be validated until the first transaction is
		// completed. This is ensured by including ns1 in the readOnlyKeys of the normal transaction,
		// resulting in ns1 also appearing in the readAndWriteKeys of the namespace lifecycle transaction,
		// thus creating a dependency. Furthermore, to ensure normal transactions following a namespace
		// lifecycle transaction are processed only post validation of the lifecycle transaction, the
		// namespace in question should be included in the readOnlyKeys of these subsequent normal
		// transactions. This method establishes a dependency from normal transactions to the namespace
		// lifecycle transaction, maintaining the correct sequence of operations.
		// For types.MetaNamespaceID, we introduce dependency to the config key in the config namespace
		// when creating new namespace. This is to establish a dependency between creating a namespace
		// and the endorsement policy for this action, that is stored in the config transaction.
		// To simplify the implementation, we introduce the dependency for any meta namespace transaction,
		// including updates.
		var key string
		switch ns.NsId {
		case types.MetaNamespaceID:
			key = constructCompositeKey(types.ConfigNamespaceID, []byte(types.ConfigKey))
		case types.ConfigNamespaceID:
			// Meta TX is dependent on the config TX, but not the other way around.
			// The above dependency for meta TX is sufficed to force an order between config and meta transactions.
		default:
			key = constructCompositeKey(types.MetaNamespaceID, []byte(ns.NsId))
		}
		if key != "" {
			readOnlyKeys = append(readOnlyKeys, key)
		}

		for _, ro := range ns.ReadsOnly {
			readOnlyKeys = append(readOnlyKeys, constructCompositeKey(ns.NsId, ro.Key))
		}

		for _, w := range ns.BlindWrites {
			writeOnlyKeys = append(writeOnlyKeys, constructCompositeKey(ns.NsId, w.Key))
		}

		for _, rw := range ns.ReadWrites {
			readAndWriteKeys = append(readAndWriteKeys, constructCompositeKey(ns.NsId, rw.Key))
		}
	}

	return &readWriteKeys{
		readsOnly:      readOnlyKeys,
		writesOnly:     writeOnlyKeys,
		readsAndWrites: readAndWriteKeys,
	}
}

func (rw *readWriteKeys) size() int {
	return len(rw.readsOnly) + len(rw.readsAndWrites) + len(rw.writesOnly)
}

// constructCompositeKey must ensures no false positives collisions in the composite key space.
func constructCompositeKey(ns string, key []byte) string {
	var sb strings.Builder
	// We pre-allocate the buffer to prevent multiple allocations.
	sb.Grow(1 + len(ns) + len(key))
	// We encode the namespace as length-value to ensure 1:1 transformation.
	// The maximum namespace length is 60, so it can fit in one byte.
	// The key length is implicit as it is encoded with the remaining bytes.
	sb.WriteByte(byte(len(ns))) //nolint:revive,nolintlint // false positive; write cannot fail.
	sb.WriteString(ns)          //nolint:revive,nolintlint // false positive; write cannot fail.
	sb.Write(key)               //nolint:revive,nolintlint // false positive; write cannot fail.
	return sb.String()
}
