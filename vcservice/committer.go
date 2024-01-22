package vcservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
)

// transactionCommitter is responsible for committing the transactions.
type transactionCommitter struct {
	ctx context.Context

	// db is a handler for the state database holding all the committed states
	db *database
	// incomingValidatedTransactions is the channel from which the committer receives the validated transactions
	// from the validator
	incomingValidatedTransactions <-chan *validatedTransactions
	// outgoingTransactionsStatus is the channel to which the committer sends the status of the transactions
	// so that the client can be notified
	outgoingTransactionsStatus chan<- *protovcservice.TransactionStatus

	metrics *perfMetrics

	wg sync.WaitGroup
}

// newCommitter creates a new transactionCommitter.
func newCommitter(
	ctx context.Context,
	db *database,
	validatedTxs <-chan *validatedTransactions,
	txsStatus chan<- *protovcservice.TransactionStatus,
	metrics *perfMetrics,
) *transactionCommitter {
	logger.Debugf("Creating committer")
	return &transactionCommitter{
		ctx:                           ctx,
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
	c.wg.Add(1)
	defer c.wg.Done()

	// NOTE: Three retry is adequate for now. We can make it configurable in the future.
	maxRetryAttempt := 3
	var attempts int
	var txsStatus *protovcservice.TransactionStatus
	var err error

	var vTx *validatedTransactions
	for {
		select {
		case <-c.ctx.Done():
			return
		case vTx = <-c.incomingValidatedTransactions:
			if vTx == nil {
				return
			}
		}

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
		select {
		case <-c.ctx.Done():
			return
		case c.outgoingTransactionsStatus <- txsStatus:
		}
		logger.Debugf("Batch of TXs sent from the committer to the output")
	}
}

func (c *transactionCommitter) commitTransactions(
	vTx *validatedTransactions,
) (*protovcservice.TransactionStatus, error) {
	// We eliminate blind writes outside the retry loop to avoid doing it more than once.
	if err := c.fillVersionForBlindWrites(vTx); err != nil {
		return nil, err
	}

	// Theoretically, we can only retry twice. Once for attempting to insert existing keys, and once for attempting
	// to reuse transaction IDs.
	// However, we still limit the number of retries to some arbitrary number to avoid an endless loop due to a bug.
	maxRetries := 1024
	for i := 0; i < maxRetries; i++ {
		// Group the writes by namespace so that we can commit to each table independently.
		info := &statesToBeCommitted{
			updateWrites: groupWritesByNamespace(vTx.validTxNonBlindWrites),
			newWrites:    groupWritesByNamespace(vTx.newWrites),
			batchStatus:  prepareStatusForCommit(vTx),
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

	// TODO: handle this case gracefully.
	panic(fmt.Errorf("[BUG] commit failed after %d retries", maxRetries))
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

// fillVersionForBlindWrites fetches the current version of the blind-writes keys, and assigns them
// to the appropriate category (new/update).
func (c *transactionCommitter) fillVersionForBlindWrites(vTx *validatedTransactions) error {
	state := make(map[NamespaceID]keyToVersion)
	for nsID, writes := range groupWritesByNamespace(vTx.validTxBlindWrites) {
		// TODO: Though we could run the following in a goroutine per namespace, we restrain
		// 		 from doing so till we evaluate the performance
		versionOfPresentKeys, err := c.db.queryVersionsIfPresent(nsID, writes.keys)
		if err != nil {
			return err
		}
		state[nsID] = versionOfPresentKeys
	}

	// Place blind writes to the appropriate map (new/update)
	for curTxID, txWrites := range vTx.validTxBlindWrites {
		for ns, nsWrites := range txWrites {
			nsState := state[ns]
			for i, key := range nsWrites.keys {
				if ver, present := nsState[string(key)]; present {
					nextVer := (VersionNumberFromBytes(ver) + 1).Bytes()
					vTx.validTxNonBlindWrites.getOrCreate(curTxID, ns).append(key, nsWrites.values[i], nextVer)
				} else {
					vTx.newWrites.getOrCreate(curTxID, ns).append(key, nsWrites.values[i], nil)
				}
			}
		}
	}

	// Clear the blind writes map
	vTx.validTxBlindWrites = make(transactionToWrites)

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
