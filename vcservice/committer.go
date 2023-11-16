package vcservice

import (
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
)

// transactionCommitter is responsible for committing the transactions.
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

// newCommitter creates a new transactionCommitter.
func newCommitter(
	db *database,
	validatedTxs <-chan *validatedTransactions,
	txsStatus chan<- *protovcservice.TransactionStatus,
	metrics *perfMetrics,
) *transactionCommitter {
	logger.Debugf("Creating committer")
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
	// NOTE: Three retry is adequate for now. We can make it configurable in future.
	maxRetryAttempt := 3
	var attempts int
	var txsStatus *protovcservice.TransactionStatus
	var err error

	for vTx := range c.incomingValidatedTransactions {
		logger.Debugf("Batch of validated TXs in the committer")
		start := time.Now()
		for attempts = 0; attempts < maxRetryAttempt; attempts++ {
			txsStatus, err = c.commitTransactions(vTx)
			if err == nil {
				break
			}

			// There are certain errors for which we need to retry the commit operation.
			// Refer to YugabyteDB documentation for retryable error.
			// Rather than distinguishing retryable transaction error, we retry for all errors.
			// This is for simplicity and we can improve it in future.
			// TODO: Add test to ensure commit is retried.
			logger.Errorf("error committing tx: %v", err)
			logger.Infof("retrying to commit transactions")
		}

		if attempts == maxRetryAttempt {
			// TODO: Handle error gracefully.
			logger.Fatalf("failed to commit transactions: %v", err)
		}

		prometheusmetrics.Observe(c.metrics.committerTxBatchLatencySeconds, time.Since(start))
		c.outgoingTransactionsStatus <- txsStatus
		logger.Debugf("Batch of TXs sent from the committer to the output")
	}
}

func (c *transactionCommitter) commitTransactions(
	vTx *validatedTransactions,
) (*protovcservice.TransactionStatus, error) {
	for {
		updateWrites, err := c.mergeWritesForCommit(vTx)
		if err != nil {
			return nil, err
		}
		info := &statesToBeCommitted{
			updateWrites: updateWrites,
			batchStatus:  prepareStatusForCommit(vTx),
			newWrites:    groupWritesByNamespace(vTx.newWrites),
		}

		mismatch, duplicated, err := c.db.commit(info)
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
	err := c.fillVersionForBlindWrites(nsToBlindWrites)
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
		vTx.validTxNonBlindWrites, vTx.validTxBlindWrites, vTx.newWrites,
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
		versionOfPresentKeys, err := c.db.queryVersionsIfPresent(nsID, writes.keys)
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
