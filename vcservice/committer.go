package vcservice

import (
	"fmt"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
)

// transactionCommitter is responsible for committing the transactions
type transactionCommitter struct {
	// db is a handler for the state database holding all the committed states
	db *database
	// incomingValidatedTransactions is the channel from which the committer receives the validated transactions
	// from the validator
	incomingValidatedTransactions <-chan *validatedTransactions
	// outgoingTransactionsStatus is the channel to which the committer sends the status of the transactions
	// so that the client can be notified
	outgoingTransactionsStatus chan<- *protovcservice.TransactionStatus

	metrics *perfMetrics
}

// versionZero is used to indicate that the key is not found in the database and hence,
// the version should be set to 0 for when the key is inserted into the database.
var versionZero = versionNumber(0).bytes()

// newCommitter creates a new transactionCommitter
func newCommitter(
	db *database,
	validatedTxs <-chan *validatedTransactions,
	txsStatus chan<- *protovcservice.TransactionStatus,
	metrics *perfMetrics,
) *transactionCommitter {
	if metrics == nil {
		metrics = newVCServiceMetrics(false)
	}
	return &transactionCommitter{
		db:                            db,
		incomingValidatedTransactions: validatedTxs,
		outgoingTransactionsStatus:    txsStatus,
		metrics:                       metrics,
	}
}

func (c *transactionCommitter) start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go c.commit()
	}
}

func (c *transactionCommitter) commit() {
	for vTx := range c.incomingValidatedTransactions {
		txsStatus, err := c.commitTransactions(vTx)
		if err != nil {
			// TODO: handle error gracefully
			panic(err)
		}
		c.outgoingTransactionsStatus <- txsStatus
	}
}

func (c *transactionCommitter) commitTransactions(
	vTx *validatedTransactions,
) (*protovcservice.TransactionStatus, error) {
	for {
		info := &commitInfo{
			batchStatus:         prepareStatusForCommit(vTx),
			newWithoutValWrites: groupWritesByNamespace(vTx.newWritesWithoutVal),
			newWithValWrites:    groupWritesByNamespace(vTx.newWritesWithVal),
		}
		updateWrites, err := c.mergeWritesForCommit(vTx)
		if err != nil {
			return nil, err
		}
		info.updateWrites = updateWrites

		t := c.metrics.newLatencyTimer("db.commit")
		mismatch, duplicated, err := c.db.commit(info)
		t.observe()
		if err != nil {
			return nil, err
		}

		if mismatch.empty() && len(duplicated) == 0 {
			return info.batchStatus, nil
		}

		if err := vTx.updateMismatch(mismatch); err != nil {
			return nil, err
		}
		vTx.updateInvalidTxs(duplicated, protoblocktx.Status_ABORTED_DUPLICATE_TXID)
	}
}

func (c *transactionCommitter) mergeWritesForCommit(vTx *validatedTransactions) (namespaceToWrites, error) {
	// Step 1: group the writes by namespace so that we can commit to each table independently
	nsToBlindWrites := groupWritesByNamespace(vTx.validTxBlindWrites)
	nsToNonBlindWrites := groupWritesByNamespace(vTx.validTxNonBlindWrites)

	// Step 2: fill the version for the blind writes
	t := c.metrics.newLatencyTimer("db.fillVersionForBlindWrites")
	err := c.fillVersionForBlindWrites(nsToBlindWrites)
	t.observe()
	if err != nil {
		return nil, err
	}

	// Step 3: merge blind and non-blind writes
	mergedWrites := make(namespaceToWrites)
	for ns, writes := range nsToBlindWrites {
		mergedWrites[ns] = writes
	}

	for ns, writes := range nsToNonBlindWrites {
		nsWrites := mergedWrites.getOrCreate(ns)
		nsWrites.appendMany(writes.keys, writes.values, writes.versions)
	}

	return mergedWrites, nil
}

// prepareStatusForCommit construct transaction status.
func prepareStatusForCommit(vTx *validatedTransactions) *protovcservice.TransactionStatus {
	txCommitStatus := &protovcservice.TransactionStatus{
		Status: map[string]protoblocktx.Status{},
	}

	for txID, status := range vTx.invalidTxIndices {
		txCommitStatus.Status[string(txID)] = status
	}
	for _, lst := range []transactionToWrites{
		vTx.validTxNonBlindWrites, vTx.validTxBlindWrites, vTx.newWritesWithoutVal, vTx.newWritesWithVal,
	} {
		for txID := range lst {
			txCommitStatus.Status[string(txID)] = protoblocktx.Status_COMMITTED
		}
	}

	return txCommitStatus
}

func (c *transactionCommitter) fillVersionForBlindWrites(nsToWrites namespaceToWrites) error {
	for nsID, writes := range nsToWrites {
		// TODO: Though we could run the following in a goroutine per namespace, we restrain
		// 		 from doing so till we evaluate the performance

		// Step 1: get the committed version for each key in the writes.
		t := c.metrics.newLatencyTimer(fmt.Sprintf("db.queryVersionsIfPresent.%d", nsID))
		versionOfPresentKeys, err := c.db.queryVersionsIfPresent(nsID, writes.keys)
		t.observe()
		if err != nil {
			return err
		}

		// Step 2: if the key is found in the database, use the committed version + 1 as the new version
		//         otherwise, use version 0.
		for i, key := range writes.keys {
			ver, notPresent := versionOfPresentKeys[string(key)]
			if !notPresent {
				writes.versions[i] = versionZero
				continue
			}

			nextVer := versionNumberFromBytes(ver) + 1
			writes.versions[i] = nextVer.bytes()
		}
	}

	return nil
}

func groupWritesByNamespace(txWrites transactionToWrites) namespaceToWrites {
	nsToWrites := make(namespaceToWrites)

	for _, nsWrites := range txWrites {
		for ns, writes := range nsWrites {
			if writes.empty() {
				continue
			}
			nsWrites := nsToWrites.getOrCreate(ns)
			nsWrites.appendMany(writes.keys, writes.values, writes.versions)
		}
	}

	return nsToWrites
}
