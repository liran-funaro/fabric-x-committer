package vcservice

import (
	"context"
	"fmt"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
	"golang.org/x/sync/errgroup"
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

func (c *transactionCommitter) run(ctx context.Context, numWorkers int) error {
	g, eCtx := errgroup.WithContext(ctx)
	for i := 0; i < numWorkers; i++ {
		g.Go(func() error {
			return c.commit(eCtx)
		})
	}

	return g.Wait()
}

func (c *transactionCommitter) commit(ctx context.Context) error {
	// NOTE: Three retry is adequate for now. We can make it configurable in the future.
	var txsStatus *protovcservice.TransactionStatus
	var err error

	incomingValidatedTransactions := channel.NewReader(ctx, c.incomingValidatedTransactions)
	outgoingTransactionsStatus := channel.NewWriter(ctx, c.outgoingTransactionsStatus)
	for {
		vTx, ok := incomingValidatedTransactions.Read()
		if !ok {
			return nil
		}

		logger.Debugf("Batch of validated TXs in the committer")
		start := time.Now()
		// There are certain errors for which we need to retry the commit operation.
		// Refer to YugabyteDB documentation for retryable error.
		// Rather than distinguishing retryable transaction error, we retry for all errors.
		// This is for simplicity and we can improve it in future.
		// TODO: Add test to ensure commit is retried.
		if retryErr := utils.Retry(func() error {
			txsStatus, err = c.commitTransactions(vTx)
			return err
		}, retryTimeout, retryInitialInterval); retryErr != nil {
			logger.Errorf("failed to commit transactions: %w", err)
			return fmt.Errorf("failed to commit transactions: %w", err)
		}

		prometheusmetrics.Observe(c.metrics.committerTxBatchLatencySeconds, time.Since(start))
		outgoingTransactionsStatus.Write(txsStatus)
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
	maxRetriesToRemoveAllInvalidTxs := 1024
	for i := 0; i < maxRetriesToRemoveAllInvalidTxs; i++ {
		// Group the writes by namespace so that we can commit to each table independently.
		info := &statesToBeCommitted{
			updateWrites: groupWritesByNamespace(vTx.validTxNonBlindWrites),
			newWrites:    groupWritesByNamespace(vTx.newWrites),
			batchStatus:  prepareStatusForCommit(vTx),
			txIDToHeight: vTx.txIDToHeight,
		}

		mismatch, duplicated, err := c.db.commit(info)
		if err != nil {
			return nil, err
		}

		if mismatch.empty() && len(duplicated) == 0 {
			// NOTE: If a submitted transaction is invalid for multiple reasons, including a duplicate
			//       transaction ID, the committer prioritizes Status_ABORTED_DUPLICATE_TXID over any other
			//       invalid status code. Even if a previously committed transaction is resubmitted (regardless
			//       of its original status), the committer will always return Status_ABORTED_DUPLICATE_TXID.
			//       The setCorrectStatusForDuplicateTxID function checks whether the transaction is truly a
			//       duplicate or a resubmission. If it is a resubmission, it retrieves the correct status from
			//       the tx_status table.
			if err := c.setCorrectStatusForDuplicateTxID(info.batchStatus, info.txIDToHeight); err != nil {
				return nil, err
			}
			return info.batchStatus, nil
		}

		if err := vTx.updateMismatch(mismatch); err != nil {
			return nil, err
		}
		vTx.updateInvalidTxs(duplicated, protoblocktx.Status_ABORTED_DUPLICATE_TXID)
	}

	return nil, fmt.Errorf("[BUG] commit failed after %d retries", maxRetriesToRemoveAllInvalidTxs)
}

// prepareStatusForCommit construct transaction status.
func prepareStatusForCommit(vTx *validatedTransactions) *protovcservice.TransactionStatus {
	txCommitStatus := &protovcservice.TransactionStatus{
		Status: map[string]protoblocktx.Status{},
	}

	for txID, status := range vTx.invalidTxStatus {
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
	state := make(map[types.NamespaceID]keyToVersion)
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
					nextVer := (types.VersionNumberFromBytes(ver) + 1).Bytes()
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

func (c *transactionCommitter) setCorrectStatusForDuplicateTxID(
	txsStatus *protovcservice.TransactionStatus,
	txIDToHeight transactionIDToHeight,
) error {
	var dupTxIDs [][]byte
	for id, s := range txsStatus.Status {
		if s == protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			dupTxIDs = append(dupTxIDs, []byte(id))
		}
	}

	if len(dupTxIDs) == 0 {
		return nil
	}

	idStatusHeight, err := c.db.readStatusWithHeight(dupTxIDs)
	if err != nil {
		return err
	}

	if idStatusHeight == nil {
		return nil
	}

	for txID, sWithHeight := range idStatusHeight {
		if types.AreSame(txIDToHeight[txID], sWithHeight.height) {
			txsStatus.Status[string(txID)] = sWithHeight.status
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
