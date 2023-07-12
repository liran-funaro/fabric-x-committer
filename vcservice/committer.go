package vcservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/yugabyte/pgx/v4"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
)

// transactionCommitter is responsible for committing the transactions
type transactionCommitter struct {
	// databaseConnection is the connection to the database
	databaseConnection *pgx.Conn
	// incomingValidatedTransactions is the channel from which the committer receives the validated transactions
	// from the validator
	incomingValidatedTransactions <-chan *validatedTransactions
	// outgoingTransactionsStatus is the channel to which the committer sends the status of the transactions
	// so that the client can be notified
	outgoingTransactionsStatus chan<- *protovcservice.TransactionStatus
}

// txIDsStatusNameSpace is the namespace for storing transaction IDs and status
// This namespace would be used to detect duplicate txIDs to avoid replay attacks
// TODO: duplicate txIDs would be handled in issue #202
const txIDsStatusNameSpace = 512

// versionZero is used to indicate that the key is not found in the database and hence,
// the version should be set to 0 for when the key is inserted into the database
var versionZero = versionNumber(0).bytes()

// newCommitter creates a new transactionCommitter
func newCommitter(
	conn *pgx.Conn,
	validatedTxs <-chan *validatedTransactions,
	txsStatus chan<- *protovcservice.TransactionStatus,
) *transactionCommitter {
	return &transactionCommitter{
		databaseConnection:            conn,
		incomingValidatedTransactions: validatedTxs,
		outgoingTransactionsStatus:    txsStatus,
	}
}

func (c *transactionCommitter) start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go c.commit()
	}
}

func (c *transactionCommitter) commit() {
	for vTx := range c.incomingValidatedTransactions {
		nsToWrites, txsStatus, err := c.prepareWritesForCommit(vTx)
		if err != nil {
			panic(err)
		}

		if err := c.commitWrites(nsToWrites); err != nil {
			panic(err)
		}

		c.outgoingTransactionsStatus <- txsStatus
	}
}

func (c *transactionCommitter) prepareWritesForCommit(
	vTx *validatedTransactions,
) (namespaceToWrites, *protovcservice.TransactionStatus, error) {
	// Step 1: group the writes by namespace so that we can commit to each table indepdenently
	nsToBlindWrites := groupWritesByNamespace(vTx.validTxBlindWrites)
	nsToNonBlindWrites := groupWritesByNamespace(vTx.validTxNonBlindWrites)

	// Step 2: fill the version for the blind writes
	if err := c.fillVersionForBlindWrites(nsToBlindWrites); err != nil {
		return nil, nil, err
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

	// Step 4: construct transaction status
	txCommitStatus := &protovcservice.TransactionStatus{
		Status: map[string]protovcservice.TransactionStatus_Flag{},
	}

	for txID := range vTx.invalidTxIndices {
		txCommitStatus.Status[string(txID)] = protovcservice.TransactionStatus_ABORTED_MVCC_CONFLICT
	}
	for txID := range vTx.validTxNonBlindWrites {
		txCommitStatus.Status[string(txID)] = protovcservice.TransactionStatus_COMMITTED
	}
	for txID := range vTx.validTxBlindWrites {
		txCommitStatus.Status[string(txID)] = protovcservice.TransactionStatus_COMMITTED
	}

	// Step 5: add the transaction status to the writes
	txIDNsWrites := &namespaceWrites{}
	for txID, status := range txCommitStatus.Status {
		txIDNsWrites.keys = append(txIDNsWrites.keys, string(txID))
		txIDNsWrites.values = append(txIDNsWrites.values, []byte{uint8(status)})
	}
	mergedWrites[txIDsStatusNameSpace] = txIDNsWrites

	return mergedWrites, txCommitStatus, nil
}

func (c *transactionCommitter) fillVersionForBlindWrites(nsToWrites namespaceToWrites) error {
	for nsID, writes := range nsToWrites {
		// TODO: Though we could run the following in a goroutine per namespace, we restrain
		// 		 from doing so till we evaluate the performance
		tableName := tableNameForNamespace(nsID)

		// Step 1: for blind writes in each namespace, query the key and version from the database
		query := fmt.Sprintf("SELECT key, version FROM %s WHERE key = ANY($1)", tableName)
		keysVers, err := c.databaseConnection.Query(context.Background(), query, writes.keys)
		if err != nil {
			return err
		}
		defer keysVers.Close()

		keys, versions, err := readKeysAndVersions(keysVers)
		if err != nil {
			return err
		}

		foundKeys := make(map[string][]byte)
		for i, key := range keys {
			foundKeys[key] = versions[i]
		}

		// Step 2: if the key is found in the database, use the committed version + 1 as the new version
		//         otherwise, use version 0
		for i, key := range writes.keys {
			ver, ok := foundKeys[key]
			if !ok {
				writes.versions[i] = versionZero
				continue
			}

			nextVer := versionNumberFromBytes(ver) + 1
			writes.versions[i] = nextVer.bytes()
		}
	}

	return nil
}

func (c *transactionCommitter) commitWrites(nsToWrites namespaceToWrites) error {
	// we want to commit all the writes to all namespaces or none at all
	// so we use a database transaction. Otherwise, the failure and recovery
	// logic will be very complicated
	tx, err := c.databaseConnection.Begin(context.Background())
	if err != nil {
		return err
	}

	// This will be executed if an error occurs. If transaction is committed, this will be a no-op
	defer func() {
		err = tx.Rollback(context.Background())
		if !errors.Is(err, pgx.ErrTxClosed) {
			logger.Warn("error rolling-back transaction: ", err)
		}
	}()

	for nsID, writes := range nsToWrites {
		query := fmt.Sprintf("SELECT %s($1::varchar[], $2::bytea[], $3::bytea[])", commitFuncNameForNamespace(nsID))

		_, err := c.databaseConnection.Exec(context.Background(), query, writes.keys, writes.values, writes.versions)
		if err != nil {
			return err
		}
	}

	if err := tx.Commit(context.Background()); err != nil {
		return err
	}

	return nil
}

func groupWritesByNamespace(txWrites transactionToWrites) namespaceToWrites {
	nsToWrites := make(namespaceToWrites)

	for _, nsWrites := range txWrites {
		for ns, writes := range nsWrites {
			nsWrites := nsToWrites.getOrCreate(ns)
			nsWrites.appendMany(writes.keys, writes.values, writes.versions)
		}
	}

	return nsToWrites
}

func tableNameForNamespace(nsID namespaceID) string {
	return fmt.Sprintf("ns_%d", nsID)
}

func validateFuncNameForNamespace(nsID namespaceID) string {
	return fmt.Sprintf("validate_reads_ns_%d", nsID)
}

func commitFuncNameForNamespace(nsID namespaceID) string {
	return fmt.Sprintf("commit_ns_%d", nsID)
}
