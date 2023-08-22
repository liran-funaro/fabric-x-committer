package dependencygraph

type (
	dependencyDetector struct {
		// readKeyToWaitingTxs holds a map of key to transaction that have read the key.
		// readKeyToWaitingTxs is used to establish write-read dependency.
		readKeyToWaitingTxs keyToTransactions

		// writeKeyToWaitingTxs holds a map of key to transaction that have written the key.
		// writeKeyToWaitingTxs is used to establish write-write and read-write dependencies.
		writeKeyToWaitingTxs keyToTransactions
	}

	keyToTransactions map[string]transactionSet
)

func newDependencyDetector() *dependencyDetector {
	return &dependencyDetector{
		readKeyToWaitingTxs:  make(keyToTransactions),
		writeKeyToWaitingTxs: make(keyToTransactions),
	}
}

// getDependenciesOf returns a list of transactions that the given transaction depends on.
// A transaction depends on another transaction if it reads a key that the other transaction has written or
// it writes a key that the other transaction has read or written. This method returns an empty list if the
// given transaction does not depend on any other transaction. The returned list does not contain duplicates.
// This method can be called from multiple goroutines concurrently provided that addWaitingTx(), removeWaitingTx(),
// and mergeWaitingTx() are not called concurrently with this method. We can use a read-write lock but
// we are avoiding it for performance reasons and leave the synchronization to the caller.
func (d *dependencyDetector) getDependenciesOf(txNode *transactionNode) transactionSet /* dependsOn */ {
	dependsOnTxs := make(transactionSet)

	for _, rk := range txNode.rwKeys.reads {
		// read-write dependency
		dependsOnTxs.update(d.writeKeyToWaitingTxs[rk])
	}

	for _, wk := range txNode.rwKeys.writes {
		// write-read dependency
		dependsOnTxs.update(d.readKeyToWaitingTxs[wk])

		// write-write dependency
		dependsOnTxs.update(d.writeKeyToWaitingTxs[wk])
	}

	return dependsOnTxs
}

// addWaitingTx adds the given transaction's reads and writes to the dependency detector
// so that getDependenciesOf() can consider them when calculating dependencies.
// This method is not thread-safe.
func (d *dependencyDetector) addWaitingTx(txNode *transactionNode) {
	d.readKeyToWaitingTxs.add(txNode.rwKeys.reads, txNode)
	d.writeKeyToWaitingTxs.add(txNode.rwKeys.writes, txNode)
}

// mergeWaitingTx merges the waiting transaction's reads and writes from
// another dependency detector. This method is not thread-safe.
func (d *dependencyDetector) mergeWaitingTx(depDetector *dependencyDetector) {
	// NOTE: The given depDetector is not modified ever after we reach here.
	//       Hence, we don't need to copy the map and can safely assign them instead.
	d.readKeyToWaitingTxs.merge(depDetector.readKeyToWaitingTxs)
	d.writeKeyToWaitingTxs.merge(depDetector.writeKeyToWaitingTxs)
}

// removeWaitingTx removes the given transaction's reads and writes from the dependency detector
// so that getDependenciesOf() does not consider them when calculating dependencies.
// This method is not thread-safe.
func (d *dependencyDetector) removeWaitingTx(txNode *transactionNode) {
	d.readKeyToWaitingTxs.remove(txNode.rwKeys.reads, txNode)
	d.writeKeyToWaitingTxs.remove(txNode.rwKeys.writes, txNode)
}

func (keyToTx keyToTransactions) add(keys []string, tx *transactionNode) {
	for _, key := range keys {
		tList, ok := keyToTx[key]
		if !ok {
			tList = make(transactionSet)
			keyToTx[key] = tList
		}

		tList[tx] = struct{}{}
	}
}

func (keyToTx keyToTransactions) remove(keys []string, tx *transactionNode) {
	for _, k := range keys {
		delete(keyToTx[k], tx)
		if len(keyToTx[k]) == 0 {
			delete(keyToTx, k)
		}
	}
}

func (keyToTx keyToTransactions) merge(inputKeyToTx keyToTransactions) {
	for key, txList := range inputKeyToTx {
		tList, ok := keyToTx[key]
		if !ok {
			// NOTE: inputKeyToTx is not modified ever after we reach here.
			//       Hence, we can safely assign txList to keyToTx[key].
			keyToTx[key] = txList
			continue
		}

		tList.update(txList)
	}
}
