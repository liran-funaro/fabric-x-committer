package vcservice

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/jackc/pgtype"
	"github.com/yugabyte/pgx/v4"
	"github.com/yugabyte/pgx/v4/pgxpool"
	"go.uber.org/zap/zapcore"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
)

const (
	// tableNameTemplate is the template for the table name for each namespace.
	tableNameTemplate = "ns_%d"

	// validateReadsSQLTemplate template for validating reads for each namespace.
	validateReadsSQLTemplate = "SELECT * FROM validate_reads_ns_%d($1::bytea[], $2::bytea[]);"
	// queryVersionsSQLTemplate template for the querying versions for given keys for each namespace.
	queryVersionsSQLTemplate = "SELECT key, version FROM %s WHERE key = ANY($1);"
	// commitTxStatusSQLTemplate template for committing transaction's status for each TX.
	commitTxStatusSQLTemplate = "SELECT commit_tx_status($1::bytea[], $2::integer[], $3::bytea[]);"
	// commitUpdateWritesSQLTemplate template for the committing updates for each namespace.
	commitUpdateWritesSQLTemplate = "SELECT commit_update_ns_%d($1::bytea[], $2::bytea[], $3::bytea[]);"
	// commitNewWritesSQLTemplate template for committing new keys for each namespace.
	commitNewWritesSQLTemplate = "SELECT commit_new_ns_%d($1::bytea[], $2::bytea[]);"
)

var (
	// ErrMetadataEmpty indicates that a requested metadata value is empty or not found.
	ErrMetadataEmpty = errors.New("metadata value is empty")
	// retryTimeout denotes the time duration for which the db operations can be retried.
	// There are certain errors for which we need to retry the query/commit operation.
	// Refer to YugabyteDB documentation for retryable error.
	retryTimeout = 30 * time.Second
	// retryInitialInterval denotes the initial interval between the retries.
	retryInitialInterval = backoff.WithInitialInterval(100 * time.Millisecond)
)

type (
	// database handles the database operations.
	database struct {
		name    string
		pool    *pgxpool.Pool
		metrics *perfMetrics
	}

	// keyToVersion is a map from key to version.
	keyToVersion map[string][]byte

	statesToBeCommitted struct {
		updateWrites   namespaceToWrites
		newWrites      namespaceToWrites
		batchStatus    *protovcservice.TransactionStatus
		txIDToHeight   transactionIDToHeight
		maxBlockNumber uint64
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
func newDatabase(config *DatabaseConfig, metrics *perfMetrics) (*database, error) {
	pool, err := NewDatabasePool(config)
	if err != nil {
		return nil, err
	}
	logger.Infof("validator persister connected to database at %s:%d", config.Host, config.Port)

	dbType, err := getDbType(context.Background(), pool)
	if err != nil {
		return nil, err
	}

	return &database{
		name:    dbType,
		pool:    pool,
		metrics: metrics,
	}, initDatabaseTables(context.Background(), pool, nil)
}

// validateNamespaceReads validates the reads for a given namespace.
func (db *database) validateNamespaceReads(nsID types.NamespaceID, r *reads) (*reads /* mismatching reads */, error) {
	// For each namespace nsID, we use the validate_reads_ns_<nsID> function to validate
	// the reads. This function returns the keys and versions of the mismatching reads.
	// Note that we have a table per namespace.
	// We have a validate function per namespace so that we can use the static SQL
	// to avoid parsing, planning and optimizing the query for each invoke. If we use
	// a common function for all namespace, we need to pass the table name as a parameter
	// which makes the query dynamic and hence we lose the benefits of static SQL.
	start := time.Now()
	query := fmt.Sprintf(validateReadsSQLTemplate, nsID)

	mismatch, err := db.retryQuery(context.Background(), query, r.keys, r.versions)
	if err != nil {
		return nil, fmt.Errorf("failed query: %w", err)
	}
	defer mismatch.Close()

	keys, values, err := readKeysAndValues[[]byte, []byte](mismatch)
	if err != nil {
		return nil, fmt.Errorf("failed reading key and version: %w", err)
	}

	mismatchingReads := &reads{}
	mismatchingReads.appendMany(keys, values)
	prometheusmetrics.Observe(db.metrics.databaseTxBatchValidationLatencySeconds, time.Since(start))

	return mismatchingReads, nil
}

// queryVersionsIfPresent queries the versions for the given keys if they exist.
func (db *database) queryVersionsIfPresent(nsID types.NamespaceID, queryKeys [][]byte) (keyToVersion, error) {
	start := time.Now()
	query := fmt.Sprintf(queryVersionsSQLTemplate, TableName(nsID))

	keysVers, err := db.retryQuery(context.Background(), query, queryKeys)
	if err != nil {
		return nil, err
	}
	defer keysVers.Close()

	foundKeys, foundVersions, err := readKeysAndValues[[]byte, []byte](keysVers)
	if err != nil {
		return nil, err
	}

	kToV := make(keyToVersion)
	for i, key := range foundKeys {
		kToV[string(key)] = foundVersions[i]
	}
	prometheusmetrics.Observe(db.metrics.databaseTxBatchQueryVersionLatencySeconds, time.Since(start))

	return kToV, nil
}

func (db *database) getLastCommittedBlockNumber(ctx context.Context) (*protoblocktx.BlockInfo, error) {
	return db.getBlockInfoMetadata(ctx, lastCommittedBlockNumberKey)
}

func (db *database) getMaxSeenBlockNumber(ctx context.Context) (*protoblocktx.BlockInfo, error) {
	return db.getBlockInfoMetadata(ctx, maxSeenBlockNumberKey)
}

func (db *database) getBlockInfoMetadata(ctx context.Context, key string) (*protoblocktx.BlockInfo, error) {
	var r pgx.Row
	var value []byte
	if retryErr := utils.Retry(func() error {
		r = db.pool.QueryRow(ctx, getMetadataPrepStmt, []byte(key))
		return r.Scan(&value)
	}, retryTimeout, retryInitialInterval); retryErr != nil {
		return nil, retryErr
	}
	if len(value) == 0 {
		return nil, ErrMetadataEmpty
	}

	return &protoblocktx.BlockInfo{Number: binary.BigEndian.Uint64(value)}, nil
}

func (db *database) queryTransactionsStatus(
	ctx context.Context,
	txIDs [][]byte,
) (*protovcservice.TransactionStatus, error) {
	txIDsStatus, err := db.retryQuery(ctx, queryTxIDsStatus, txIDs)
	if err != nil {
		return nil, err
	}
	defer txIDsStatus.Close()

	txIDs, status, err := readKeysAndValues[[]byte, int32](txIDsStatus)
	if err != nil {
		return nil, err
	}

	s := &protovcservice.TransactionStatus{
		Status: make(map[string]protoblocktx.Status),
	}

	for i, txID := range txIDs {
		s.Status[string(txID)] = protoblocktx.Status(status[i])
	}

	return s, nil
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
	if retryErr := utils.Retry(func() error {
		_, err := db.pool.Exec(ctx, setMetadataPrepStmt, []byte(lastCommittedBlockNumberKey), v)
		return err
	}, retryTimeout, retryInitialInterval); retryErr != nil {
		return fmt.Errorf("failed to set the last committed block number: %w", retryErr)
	}

	return nil
}

// commit commits the writes to the database.
func (db *database) commit(states *statesToBeCommitted) (namespaceToReads, []TxID, error) {
	start := time.Now()
	if states.empty() {
		return nil, nil, nil
	}

	// We want to commit all the writes to all namespaces or none at all,
	// so we use a database transaction. Otherwise, the failure and recovery
	// logic will be very complicated.
	ctx := context.Background()
	tx, rollBackFunc, err := db.beginTx(context.Background())
	if err != nil {
		return nil, nil, err
	}

	// This will be executed if an error occurs. If transaction is committed, this will be a no-op.
	defer rollBackFunc()

	mismatched, duplicated, err := db.commitStatesByGroup(ctx, tx, states)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx commitInner: %w", err)
	}
	if !mismatched.empty() || len(duplicated) > 0 {
		// rollback
		return mismatched, duplicated, nil
	}

	err = tx.Commit(ctx)
	prometheusmetrics.Observe(db.metrics.databaseTxBatchCommitLatencySeconds, time.Since(start))
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx commit: %w", err)
	}

	return nil, nil, nil
}

func (db *database) commitStatesByGroup(
	ctx context.Context,
	tx pgx.Tx,
	states *statesToBeCommitted,
) (namespaceToReads, []TxID, error) {
	mismatched, err := db.commitNewKeys(tx, states.newWrites)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx commitNewKeys: %w", err)
	}

	if !mismatched.empty() {
		// Since a mismatch causes a rollback, we fail fast.
		return mismatched, nil, nil
	}

	duplicated, err := db.commitTxStatus(ctx, tx, states)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx execCommitTxStatus: %w", err)
	}

	if len(duplicated) > 0 {
		// Since a duplicate ID causes a rollback, we fail fast.
		return nil, duplicated, nil
	}

	// Updates cannot have a mismatch because their versions are validated beforehand.
	if err = db.commitUpdates(ctx, tx, states.updateWrites); err != nil {
		return nil, nil, fmt.Errorf("failed tx execCommitUpdate: %w", err)
	}

	// NOTE: As we store an integer and allow comparisons, we must ensure consistent byte representation.
	//       This means using the same length value and big-endian byte ordering for all stored integers.
	//       Both PostgreSQL and YugabyteDB support comparison of big-endian bytes through the BYTEA data type
	//       and standard comparison operators.
	v := make([]byte, 8)
	binary.BigEndian.PutUint64(v, states.maxBlockNumber)
	if _, err := tx.Exec(ctx, setMetadataIfGreaterPrepStmt, []byte(maxSeenBlockNumberKey), v); err != nil {
		return nil, nil, fmt.Errorf("failed to set the last seen max block number: %w", err)
	}

	return nil, nil, nil
}

func (db *database) commitTxStatus(
	ctx context.Context,
	tx pgx.Tx,
	states *statesToBeCommitted,
) ([]TxID /* duplicated */, error) {
	start := time.Now()
	if states.batchStatus == nil || len(states.batchStatus.Status) == 0 {
		return nil, nil
	}

	numEntries := len(states.batchStatus.Status)
	ids := make([][]byte, 0, numEntries)
	statues := make([]int, 0, numEntries)
	heights := make([][]byte, 0, numEntries)
	for tID, status := range states.batchStatus.Status {
		// We cannot commit a "duplicated ID" status since we already have a status entry with this ID.
		if status == protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			continue
		}
		ids = append(ids, []byte(tID))
		statues = append(statues, int(status))
		blkAndTxNum, ok := states.txIDToHeight[TxID(tID)]
		if !ok {
			return nil, fmt.Errorf("block and tx number is not passed for txID %s", tID)
		}
		heights = append(heights, blkAndTxNum.ToBytes())
	}

	ret := tx.QueryRow(ctx, commitTxStatusSQLTemplate, ids, statues, heights)
	duplicated, err := readInsertResult(ret, ids)
	if err != nil {
		return nil, fmt.Errorf("failed fetching results from query: %w", err)
	}
	if len(duplicated) == 0 {
		prometheusmetrics.Observe(db.metrics.databaseTxBatchCommitTxsStatusLatencySeconds, time.Since(start))
		return nil, nil
	}

	duplicatedTx := make([]TxID, len(duplicated))
	for i, v := range duplicated {
		duplicatedTx[i] = TxID(v)
	}
	prometheusmetrics.Observe(db.metrics.databaseTxBatchCommitTxsStatusLatencySeconds, time.Since(start))
	return duplicatedTx, nil
}

func (db *database) commitNewKeys(
	tx pgx.Tx, nsToWrites namespaceToWrites,
) (namespaceToReads /* mismatched */, error) {
	start := time.Now()
	defer func() {
		prometheusmetrics.Observe(
			db.metrics.databaseTxBatchCommitInsertNewKeyWithValueLatencySeconds,
			time.Since(start),
		)
	}()

	mismatch := make(namespaceToReads)
	for nsID, writes := range nsToWrites {
		if writes.empty() {
			continue
		}

		q := fmt.Sprintf(commitNewWritesSQLTemplate, nsID)
		ret := tx.QueryRow(context.Background(), q, writes.keys, writes.values)
		violating, err := readInsertResult(ret, writes.keys)
		if err != nil {
			return nil, fmt.Errorf("failed fetching results from query: %w", err)
		}

		if len(violating) > 0 {
			// We can use arbitrary versions from the list of writes since they are all 'nil'.
			mismatch.getOrCreate(nsID).appendMany(violating, writes.versions[:len(violating)])
		}
	}

	if len(mismatch) > 0 {
		return mismatch, nil
	}

	return nil, db.createTablesAndFunctionsForNamespace(tx, nsToWrites[types.MetaNamespaceID])
}

func (db *database) commitUpdates(ctx context.Context, tx pgx.Tx, nsToWrites namespaceToWrites) error {
	start := time.Now()
	for nsID, writes := range nsToWrites {
		if writes.empty() {
			continue
		}

		query := fmt.Sprintf(commitUpdateWritesSQLTemplate, nsID)
		_, err := tx.Exec(ctx, query, writes.keys, writes.values, writes.versions)
		if err != nil {
			return fmt.Errorf("failed tx exec: %w", err)
		}
	}
	prometheusmetrics.Observe(db.metrics.databaseTxBatchCommitUpdateLatencySeconds, time.Since(start))

	return nil
}

func (db *database) createTablesAndFunctionsForNamespace(tx pgx.Tx, newNs *namespaceWrites) error {
	if newNs == nil {
		return nil
	}

	for _, ns := range newNs.keys {
		nsID, err := types.NamespaceIDFromBytes(ns)
		if err != nil {
			return err
		}

		tableName := TableName(nsID)
		for _, stmt := range initStatementsWithTemplate {
			if _, err := tx.Exec(context.Background(), stmtFmt(stmt, tableName, db.name)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (db *database) beginTx(ctx context.Context) (pgx.Tx, func(), error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx begin: %w", err)
	}

	return tx, func() {
		rollbackErr := tx.Rollback(context.Background())
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed rolling-back transaction: ", rollbackErr)
		}
	}, nil
}

func (s *statesToBeCommitted) empty() bool {
	return s.updateWrites.empty() && s.newWrites.empty() && (s.batchStatus == nil || len(s.batchStatus.Status) == 0)
}

func (db *database) close() {
	db.pool.Close()
}

// readKeysAndValues reads the keys and values from the given rows.
func readKeysAndValues[K, V any](r pgx.Rows) ([]K, []V, error) {
	var keys []K
	var values []V

	for r.Next() {
		var key K
		var value V
		if err := r.Scan(&key, &value); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		values = append(values, value)
	}

	return keys, values, r.Err()
}

func readInsertResult(r pgx.Row, allKeys [][]byte) ([][]byte, error) {
	var res []pgtype.Value
	if err := r.Scan(&res); err != nil {
		return nil, fmt.Errorf("failed scan: %w", err)
	}

	var result string
	if err := res[0].AssignTo(&result); err != nil {
		return nil, fmt.Errorf("failed reading result: %w", err)
	}
	switch result {
	case "success":
		return nil, nil
	case "all-unique-violation":
		return allKeys, nil
	default:
	}

	var violating [][]byte
	if err := res[1].AssignTo(&violating); err != nil {
		return nil, fmt.Errorf("failed reading violating: %w", err)
	}

	return violating, nil
}

// TableName returns the table name for the given namespace.
func TableName(nsID types.NamespaceID) string {
	return fmt.Sprintf(tableNameTemplate, nsID)
}

func (db *database) retryQuery(ctx context.Context, sql string, arg ...any) (pgx.Rows, error) { // nolint:ireturn
	var rows pgx.Rows
	retryErr := utils.Retry(func() error {
		var err error
		rows, err = db.pool.Query(ctx, sql, arg...)
		return err
	}, retryTimeout, retryInitialInterval)
	return rows, retryErr
}

type statusWithHeight struct {
	status protoblocktx.Status
	height *types.Height
}

func (db *database) readStatusWithHeight(txIDs [][]byte) (map[TxID]*statusWithHeight, error) {
	var r pgx.Rows
	if retryErr := utils.Retry(func() error {
		var err error
		r, err = db.pool.Query(context.Background(), queryTxStatusSQLTemplate, txIDs)
		return err
	}, retryTimeout, retryInitialInterval); retryErr != nil {
		return nil, fmt.Errorf("error reading status for txIDs: %w", retryErr)
	}
	defer r.Close()

	rows := make(map[TxID]*statusWithHeight)
	for r.Next() {
		var id []byte
		var status int32
		var height []byte

		if err := r.Scan(&id, &status, &height); err != nil {
			return nil, err
		}

		ht, _, err := types.NewHeightFromBytes(height)
		if err != nil {
			return nil, err
		}

		rows[TxID(id)] = &statusWithHeight{status: protoblocktx.Status(status), height: ht}
	}
	return rows, nil
}
