/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package vc

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/yugabyte/pgx/v4"
	"github.com/yugabyte/pgx/v4/pgxpool"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/hyperledger/fabric-x-committer/api/protoblocktx"
	"github.com/hyperledger/fabric-x-committer/api/types"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring/promutil"
)

const (
	// tableNameTempl is the template for the table name for each namespace.
	tableNameTempl = "ns_%s"

	// validateReadsSQLTempl template for validating reads for each namespace.
	validateReadsSQLTempl = "SELECT * FROM " + validateReadsOnNsFuncNamePrefix + tableNameTempl +
		"($1::bytea[], $2::bigint[]);"
	// updateNsStatesSQLTempl template for the committing updates for each namespace.
	updateNsStatesSQLTempl = "SELECT " + updateNsStatesFuncNamePrefix + tableNameTempl +
		"($1::bytea[], $2::bytea[], $3::bigint[]);"
	// insertNsStatesSQLTempl template for committing new keys for each namespace.
	insertNsStatesSQLTempl = "SELECT " + insertNsStatesFuncNamePrefix + tableNameTempl + "($1::bytea[], $2::bytea[]);"

	// queryVersionsSQLTempl template for the querying versions for given keys for each namespace.
	queryVersionsSQLTempl = "SELECT key, version FROM " + tableNameTempl + " WHERE key = ANY($1);"
	// insertTxStatusSQLTempl template for committing transaction's status for each TX.
	insertTxStatusSQLTempl = "SELECT " + insertTxStatusFuncName + "($1::bytea[], $2::integer[], $3::bytea[]);"

	queryPoliciesSQLStmt = "SELECT key, value from ns_" + types.MetaNamespaceID + ";"
	queryConfigSQLStmt   = "SELECT key, value from ns_" + types.ConfigNamespaceID + ";"
)

// ErrMetadataEmpty indicates that a requested metadata value is empty or not found.
var ErrMetadataEmpty = errors.New("metadata value is empty")

type (
	// database handles the database operations.
	database struct {
		pool    *pgxpool.Pool
		metrics *perfMetrics
		retry   *connection.RetryProfile
	}

	// keyToVersion is a map from key to version.
	keyToVersion map[string]uint64

	statesToBeCommitted struct {
		updateWrites namespaceToWrites
		newWrites    namespaceToWrites
		batchStatus  *protoblocktx.TransactionsStatus
		txIDToHeight transactionIDToHeight
	}
)

func (s *statesToBeCommitted) Debug() {
	if logger.Level() > zapcore.DebugLevel {
		return
	}
	logger.Debugf("total states: %d\n\tupdate: %d\n\twrites: %d\n\tbatch status: %d",
		len(s.updateWrites)+len(s.newWrites)+len(s.batchStatus.Status),
		len(s.updateWrites), len(s.newWrites), len(s.batchStatus.Status))
}

// newDatabase creates a new database.
func newDatabase(ctx context.Context, config *DatabaseConfig, metrics *perfMetrics) (*database, error) {
	pool, err := NewDatabasePool(ctx, config)
	if err != nil {
		return nil, err
	}

	logger.Infof("validator persister connected to database at [%s]", config.EndpointsString())

	defer func() {
		if err != nil {
			pool.Close()
		}
	}()

	return &database{
		pool:    pool,
		metrics: metrics,
		retry:   config.Retry,
	}, nil
}

func (db *database) close() {
	logger.Info("closing database connection")
	db.pool.Close()
}

// validateNamespaceReads validates the reads for a given namespace.
func (db *database) validateNamespaceReads(
	ctx context.Context,
	nsID string,
	r *reads,
) (*reads /* read conflicts */, error) {
	// For each namespace nsID, we use the validate_reads_ns_<nsID> function to validate
	// the reads. This function returns the keys and versions of the conflicting reads.
	// Note that we have a table per namespace.
	// We have a validate function per namespace so that we can use the static SQL
	// to avoid parsing, planning and optimizing the query for each invoke. If we use
	// a common function for all namespace, we need to pass the table name as a parameter
	// which makes the query dynamic and hence we lose the benefits of static SQL.
	start := time.Now()
	query := fmt.Sprintf(validateReadsSQLTempl, nsID)

	conflictIdx, err := retryQueryAndReadArrayResult[int](ctx, db, query, r.keys, r.versions)
	if err != nil {
		return nil, fmt.Errorf("failed to validate reads on namespace [%s]: %w", nsID, err)
	}
	readConflicts := &reads{}
	for _, i := range conflictIdx {
		// SQL indexing starts from 1.
		readConflicts.append(r.keys[i-1], r.versions[i-1])
	}
	promutil.Observe(db.metrics.databaseTxBatchValidationLatencySeconds, time.Since(start))

	return readConflicts, nil
}

// queryVersionsIfPresent queries the versions for the given keys if they exist.
func (db *database) queryVersionsIfPresent(ctx context.Context, nsID string, queryKeys [][]byte) (keyToVersion, error) {
	start := time.Now()
	query := fmt.Sprintf(queryVersionsSQLTempl, nsID)

	foundKeys, foundVersions, err := retryQueryAndReadTwoItems[[]byte, int64](ctx, db, query, queryKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to get keys' version from namespace [%s]: %w", nsID, err)
	}

	kToV := make(keyToVersion)
	for i, key := range foundKeys {
		kToV[string(key)] = uint64(foundVersions[i]) //nolint:gosec // DB table is constraint to non-negative value.
	}
	promutil.Observe(db.metrics.databaseTxBatchQueryVersionLatencySeconds, time.Since(start))

	return kToV, nil
}

func (db *database) getLastCommittedBlockNumber(ctx context.Context) (*protoblocktx.LastCommittedBlock, error) {
	blkInfo, err := db.getBlockInfoMetadata(ctx, lastCommittedBlockNumberKey)
	if err != nil && !errors.Is(err, ErrMetadataEmpty) {
		return nil, fmt.Errorf("failed to get the last committed block number: %w", err)
	}
	return &protoblocktx.LastCommittedBlock{Block: blkInfo}, nil
}

func (db *database) getBlockInfoMetadata(ctx context.Context, key string) (*protoblocktx.BlockInfo, error) {
	var r pgx.Row
	var value []byte
	if retryErr := db.retry.Execute(ctx, func() error {
		r = db.pool.QueryRow(ctx, getMetadataPrepSQLStmt, []byte(key))
		return errors.Wrapf(r.Scan(&value), "failed to get key [%s] using query [%s]", key, getMetadataPrepSQLStmt)
	}); retryErr != nil {
		return nil, retryErr
	}
	if len(value) == 0 {
		return nil, ErrMetadataEmpty
	}
	return &protoblocktx.BlockInfo{Number: binary.BigEndian.Uint64(value)}, nil
}

func (db *database) setLastCommittedBlockNumber(ctx context.Context, bInfo *protoblocktx.BlockInfo) error {
	// NOTE: We can actually batch this transaction with regular user transactions and perform
	//       a single commit. However, we need to implement special logic to handle cases
	//       when there are no waiting user transactions. Hence, for simplicity, we are not
	//       batching this transaction with regular user transactions. After performance
	//       benchmarking, if there is a significant benefit in performing the batching,
	//       we will implement further optimizations.

	// NOTE: As we store an integer and allow comparisons, we must ensure consistent byte representation.
	//       This means using the same length value and big-endian byte ordering for all stored integers.
	//       Both PostgreSQL and YugabyteDB support comparison of big-endian bytes through the BYTEA data type
	//       and standard comparison operators.
	v := make([]byte, 8)
	binary.BigEndian.PutUint64(v, bInfo.Number)
	return db.retry.ExecuteSQL(ctx, db.pool, setMetadataPrepSQLStmt, []byte(lastCommittedBlockNumberKey), v)
}

// commit commits the writes to the database.
func (db *database) commit(ctx context.Context, states *statesToBeCommitted) (namespaceToReads, []TxID, error) {
	start := time.Now()
	if states.empty() {
		return nil, nil, nil
	}

	// We want to commit all the writes to all namespaces or none at all,
	// so we use a database transaction. Otherwise, the failure and recovery
	// logic will be very complicated.
	tx, rollBackFunc, err := db.beginTx(ctx)
	if err != nil {
		return nil, nil, err
	}

	// This will be executed if an error occurs. If transaction is committed, this will be a no-op.
	defer rollBackFunc()

	conflicts, duplicates, err := db.writeStatesByGroup(ctx, tx, states)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write states: %w", err)
	}
	if !conflicts.empty() || len(duplicates) > 0 {
		// rollback
		return conflicts, duplicates, nil
	}

	err = tx.Commit(ctx)
	promutil.Observe(db.metrics.databaseTxBatchCommitLatencySeconds, time.Since(start))
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to perform the final commit on the database transaction")
	}

	return nil, nil, nil
}

func (db *database) writeStatesByGroup(
	ctx context.Context,
	tx pgx.Tx,
	states *statesToBeCommitted,
) (namespaceToReads, []TxID, error) {
	// Because the coordinator might submit duplicate transactions during connection issues,
	// we must insert transaction IDs first. This allows us to detect conflicts early,
	// as another vcservice instance might have already committed the same transaction.
	// If we don't insert transaction IDs first, there are other consequences. These
	// could be mitigated by adding writes with a null version present in BlindWrites
	// to the readToTxIDs map, but inserting the IDs upfront is a cleaner solution.
	duplicates, err := db.insertTxStatus(ctx, tx, states)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to insert transactions status: %w", err)
	}

	if len(duplicates) > 0 {
		// Since a duplicate ID causes a rollback, we fail fast.
		logger.Debugf("%d duplicate keys were found", len(duplicates))
		return nil, duplicates, nil
	}

	conflicts, err := db.insertStates(ctx, tx, states.newWrites)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to insert states: %w", err)
	}

	if !conflicts.empty() {
		// Since a conflicts causes a rollback, we fail fast.
		logger.Debugf("%d read conflicts were found", len(conflicts))
		return conflicts, nil, nil
	}

	if err = createTablesAndFunctionsForNamespaces(ctx, tx, states.newWrites[types.MetaNamespaceID]); err != nil {
		return nil, nil, fmt.Errorf("failed to create tables and functions for new namespaces: %w", err)
	}

	// Updates cannot have a conflicts because their versions are validated beforehand.
	if err = db.updateStates(ctx, tx, states.updateWrites); err != nil {
		return nil, nil, fmt.Errorf("failed to execute updates: %w", err)
	}

	return nil, nil, nil
}

func (db *database) insertTxStatus(
	ctx context.Context,
	tx pgx.Tx,
	states *statesToBeCommitted,
) ([]TxID /* duplicates */, error) {
	start := time.Now()
	if states.batchStatus == nil || len(states.batchStatus.Status) == 0 {
		return nil, nil
	}

	numEntries := len(states.batchStatus.Status)
	ids := make([][]byte, 0, numEntries)
	statues := make([]int, 0, numEntries)
	heights := make([][]byte, 0, numEntries)
	for tID, status := range states.batchStatus.Status {
		// We cannot insert a "duplicate ID" status since we already have a status entry with this ID.
		if status.Code == protoblocktx.Status_REJECTED_DUPLICATE_TX_ID {
			continue
		}
		ids = append(ids, []byte(tID))
		statues = append(statues, int(status.Code))
		blkAndTxNum, ok := states.txIDToHeight[TxID(tID)]
		if !ok {
			return nil, errors.Newf("block and tx number are not passed for txID [%s]", tID)
		}
		heights = append(heights, blkAndTxNum.ToBytes())
	}

	ret := tx.QueryRow(ctx, insertTxStatusSQLTempl, ids, statues, heights)
	duplicates, err := readArrayResult[[]byte](ret)
	if err != nil {
		return nil, fmt.Errorf("failed to read result from query [%s]: %w", insertTxStatusSQLTempl, err)
	}
	if len(duplicates) == 0 {
		promutil.Observe(db.metrics.databaseTxBatchCommitTxsStatusLatencySeconds, time.Since(start))
		return nil, nil
	}

	duplicateTxs := make([]TxID, len(duplicates))
	for i, v := range duplicates {
		duplicateTxs[i] = TxID(v)
	}
	logger.Debugf("Total number of duplicate txs: %d", len(duplicateTxs))
	promutil.Observe(db.metrics.databaseTxBatchCommitTxsStatusLatencySeconds, time.Since(start))
	return duplicateTxs, nil
}

func (db *database) insertStates(
	ctx context.Context, tx pgx.Tx, nsToWrites namespaceToWrites,
) (namespaceToReads /* conflicts */, error) {
	start := time.Now()
	defer func() {
		promutil.Observe(
			db.metrics.databaseTxBatchCommitInsertNewKeyWithValueLatencySeconds,
			time.Since(start),
		)
	}()

	conflicts := make(namespaceToReads)
	for nsID, writes := range nsToWrites {
		if writes.empty() {
			continue
		}

		q := fmt.Sprintf(insertNsStatesSQLTempl, nsID)
		ret := tx.QueryRow(ctx, q, writes.keys, writes.values)
		violating, err := readArrayResult[[]byte](ret)
		if err != nil {
			return nil, fmt.Errorf("failed to read result from query [%s]: %w", q, err)
		}

		if len(violating) > 0 {
			// We can use arbitrary versions from the list of writes since they are all 'nil'.
			logger.Debugf("Total number of conflicts: %d for namespace [%s]", len(conflicts), nsID)
			conflicts.getOrCreate(nsID).appendMany(violating, make([]*uint64, len(violating)))
		}
	}

	if len(conflicts) > 0 {
		logger.Debugf("For all namespaces: Total number of conflicts: %v", len(conflicts))
		return conflicts, nil
	}

	return nil, nil
}

func (db *database) updateStates(ctx context.Context, tx pgx.Tx, nsToWrites namespaceToWrites) error {
	start := time.Now()
	for nsID, writes := range nsToWrites {
		if writes.empty() {
			continue
		}

		query := fmt.Sprintf(updateNsStatesSQLTempl, nsID)
		_, err := tx.Exec(ctx, query, writes.keys, writes.values, writes.versions)
		if err != nil {
			return errors.Wrapf(err, "failed to execute query [%s]", query)
		}
	}
	promutil.Observe(db.metrics.databaseTxBatchCommitUpdateLatencySeconds, time.Since(start))

	return nil
}

func createTablesAndFunctionsForNamespaces(ctx context.Context, tx pgx.Tx, newNs *namespaceWrites) error {
	if newNs == nil {
		return nil
	}

	for _, ns := range newNs.keys {
		nsID := string(ns)

		tableName := TableName(nsID)
		logger.Infof("Creating table [%s] and required functions for namespace [%s]", tableName, ns)
		err := createNsTables(nsID, func(q string) error {
			_, execErr := tx.Exec(ctx, q)
			return execErr
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (db *database) beginTx(ctx context.Context) (pgx.Tx, func(), error) {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to being a database transaction")
	}

	return tx, func() { //nolint:contextcheck // we want to rollback changes even when ctx gets cancelled.
		rollbackErr := tx.Rollback(context.Background())
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed rolling-back transaction: ", rollbackErr)
		}
	}, nil
}

func (s *statesToBeCommitted) empty() bool {
	return s.updateWrites.empty() && s.newWrites.empty() && (s.batchStatus == nil || len(s.batchStatus.Status) == 0)
}

// TableName returns the table name for the given namespace.
func TableName(nsID string) string {
	return fmt.Sprintf(tableNameTempl, nsID)
}

func (db *database) readStatusWithHeight(
	ctx context.Context,
	txIDs [][]byte,
) (map[string]*protoblocktx.StatusWithHeight, error) {
	var rows map[string]*protoblocktx.StatusWithHeight
	retryErr := db.retry.Execute(ctx, func() error {
		r, err := db.pool.Query(ctx, queryTxIDsStatusPrepSQLStmt, txIDs)
		if err != nil {
			return errors.Wrap(err, "failed to query txIDs from the table [tx_status]")
		}
		defer r.Close()

		// reset map every retry
		rows = make(map[string]*protoblocktx.StatusWithHeight)
		for r.Next() {
			var id []byte
			var status int32
			var height []byte

			if err = r.Scan(&id, &status, &height); err != nil {
				return errors.Wrapf(err, "failed to read rows from the query [%s] result", queryTxIDsStatusPrepSQLStmt)
			}

			ht, _, err := types.NewHeightFromBytes(height)
			if err != nil {
				return fmt.Errorf("failed to create height: %w", err)
			}

			rows[string(id)] = types.CreateStatusWithHeight(protoblocktx.Status(status), ht.BlockNum, int(ht.TxNum))
		}
		return errors.Wrap(r.Err(), "error occurred while reading rows")
	})

	return rows, retryErr
}

func (db *database) readNamespacePolicies(ctx context.Context) (*protoblocktx.NamespacePolicies, error) {
	keys, values, err := retryQueryAndReadTwoItems[[]byte, []byte](ctx, db, queryPoliciesSQLStmt)
	if err != nil {
		return nil, fmt.Errorf("failed to read the policies from table [%s]: %w", TableName(types.MetaNamespaceID), err)
	}
	policy := &protoblocktx.NamespacePolicies{
		Policies: make([]*protoblocktx.PolicyItem, len(keys)),
	}
	for i, key := range keys {
		policy.Policies[i] = &protoblocktx.PolicyItem{
			Namespace: string(key),
			Policy:    values[i],
		}
	}
	return policy, nil
}

func (db *database) readConfigTX(ctx context.Context) (*protoblocktx.ConfigTransaction, error) {
	_, values, err := retryQueryAndReadTwoItems[[]byte, []byte](ctx, db, queryConfigSQLStmt)
	if err != nil {
		return nil, fmt.Errorf("failed to read the config transaction from table [%s]: %w",
			TableName(types.ConfigNamespaceID), err)
	}
	configTX := &protoblocktx.ConfigTransaction{}
	for _, v := range values {
		configTX.Envelope = v
	}
	return configTX, nil
}

func retryQueryAndReadArrayResult[T any](
	ctx context.Context, db *database, query string, args ...any,
) (items []T, retryErr error) {
	retryErr = db.retry.Execute(ctx, func() error {
		row := db.pool.QueryRow(ctx, query, args...)
		var readErr error
		items, readErr = readArrayResult[T](row)
		if readErr != nil {
			logger.Debugf("failed attempt: %s", readErr)
		}
		return errors.Wrapf(readErr, "failed to read rows from the query [%s] results", query)
	})
	return items, retryErr
}

func retryQueryAndReadTwoItems[T1, T2 any](
	ctx context.Context, db *database, query string, args ...any,
) (items1 []T1, items2 []T2, err error) {
	retryErr := db.retry.Execute(ctx, func() error {
		rows, queryErr := db.pool.Query(ctx, query, args...)
		if queryErr != nil {
			return errors.Wrapf(queryErr, "failed to query rows: query [%s]", query)
		}
		defer rows.Close()
		var readErr error
		items1, items2, readErr = readTwoItems[T1, T2](rows)
		if readErr != nil {
			logger.WithOptions(zap.AddCallerSkip(8)).Debugf("failed attempt: %s", readErr)
		}
		return errors.Wrapf(readErr, "failed to read rows from the query [%s] results", query)
	})
	return items1, items2, retryErr
}

// readTwoItems reads two items from given rows.
func readTwoItems[T1, T2 any](r pgx.Rows) (items1 []T1, items2 []T2, err error) {
	for r.Next() {
		var i1 T1
		var i2 T2
		if err := r.Scan(&i1, &i2); err != nil {
			return nil, nil, errors.Wrap(err, "failed while scanning a row")
		}
		items1 = append(items1, i1)
		items2 = append(items2, i2)
	}
	return items1, items2, errors.Wrap(r.Err(), "failed while reading from rows")
}

func readArrayResult[T any](r pgx.Row) (res []T, err error) {
	err = r.Scan(&res)
	return res, errors.Wrap(err, "failed while scanning a row")
}
