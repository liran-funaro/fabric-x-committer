package vcservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgtype"
	"github.com/yugabyte/pgx/v4"
	"github.com/yugabyte/pgx/v4/pgxpool"
	"go.uber.org/zap/zapcore"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
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
	commitTxStatusSQLTemplate = "SELECT commit_tx_status($1::bytea[], $2::integer[]);"
	// commitUpdateWritesSQLTemplate template for the committing updates for each namespace.
	commitUpdateWritesSQLTemplate = "SELECT commit_update_ns_%d($1::bytea[], $2::bytea[], $3::bytea[]);"
	// commitNewWritesSQLTemplate template for committing new keys for each namespace.
	commitNewWritesSQLTemplate = "SELECT commit_new_ns_%d($1::bytea[], $2::bytea[]);"
)

// ErrNoBlockCommitted occurs when getLastCommittedBlockNumber is called when the
// block 0 itself is not yet committed.
var ErrNoBlockCommitted = errors.New("no block is committed")

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
		updateWrites namespaceToWrites
		newWrites    namespaceToWrites
		batchStatus  *protovcservice.TransactionStatus
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

	dbType, err := getDbType(context.Background(), pool)
	if err != nil {
		return nil, err
	}

	// TODO: We need to create all system namespaces such as txStatus
	//       and namespaceLifecycle here. This requires refactoring of dbinit.go.

	return &database{
		name:    dbType,
		pool:    pool,
		metrics: metrics,
	}, nil
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

	mismatch, err := db.pool.Query(context.Background(), query, r.keys, r.versions)
	if err != nil {
		return nil, fmt.Errorf("failed query: %w", err)
	}
	defer mismatch.Close()

	keys, values, err := readKeysAndVersions(mismatch)
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
	keysVers, err := db.pool.Query(context.Background(), query, queryKeys)
	if err != nil {
		return nil, err
	}
	defer keysVers.Close()

	foundKeys, foundVersions, err := readKeysAndVersions(keysVers)
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

func (db *database) setLastCommittedBlockNumber(ctx context.Context, number uint64) error {
	// NOTE: We can actually batch this transaction with regular user transactions and perform
	//       a single commit. However, we need to implement special logic to handle cases
	//       when there are no waiting user transactions. Hence, for simplicity, we are not
	//       batching this transaction with regular user transactions. After performance
	//       benchmarking, if there is a significant benefit in performing the batching,
	//       we will implement further optimizations.
	if _, err := db.pool.Exec(
		ctx,
		setMetadataPrepStmt,
		[]byte(lastCommittedBlockNumberKey),
		strconv.FormatUint(number, 10),
	); err != nil {
		return fmt.Errorf("failed to set the last committed block number: %w", err)
	}

	return nil
}

func (db *database) getLastCommittedBlockNumber(ctx context.Context) (*protoblocktx.LastCommittedBlock, error) {
	r := db.pool.QueryRow(ctx, getMetadataPrepStmt, []byte(lastCommittedBlockNumberKey))
	var value []byte
	if err := r.Scan(&value); err != nil {
		return nil, err
	}
	if len(value) == 0 {
		return nil, ErrNoBlockCommitted
	}
	blkNumber, err := strconv.ParseUint(string(value), 10, 64)
	if err != nil {
		return nil, err
	}
	return &protoblocktx.LastCommittedBlock{Number: blkNumber}, nil
}

// commit commits the writes to the database.
func (db *database) commit(states *statesToBeCommitted) (namespaceToReads, []txID, error) {
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
) (namespaceToReads, []txID, error) {
	mismatched, err := db.commitNewKeys(tx, states.newWrites)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx commitNewKeys: %w", err)
	}

	if !mismatched.empty() {
		// Since a mismatch causes a rollback, we fail fast.
		return mismatched, nil, nil
	}

	duplicated, err := db.commitTxStatus(ctx, tx, states.batchStatus)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx execCommitTxStatus: %w", err)
	}

	if len(duplicated) > 0 {
		// Since a duplicate ID causes a rollback, we fail fast.
		return mismatched, duplicated, nil
	}

	// Updates cannot have a mismatch because their versions are validated beforehand.
	if err = db.commitUpdates(ctx, tx, states.updateWrites); err != nil {
		return nil, nil, fmt.Errorf("failed tx execCommitUpdate: %w", err)
	}

	return mismatched, duplicated, nil
}

func (db *database) commitTxStatus(
	ctx context.Context, tx pgx.Tx, batchStatus *protovcservice.TransactionStatus,
) ([]txID /* duplicated */, error) {
	start := time.Now()
	if batchStatus == nil || len(batchStatus.Status) == 0 {
		return nil, nil
	}

	ids := make([][]byte, 0, len(batchStatus.Status))
	statues := make([]int, 0, len(batchStatus.Status))
	for txID, status := range batchStatus.Status {
		// We cannot commit a "duplicated ID" status since we already have a status entry with this ID.
		if status == protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			continue
		}
		ids = append(ids, []byte(txID))
		statues = append(statues, int(status))
	}

	ret := tx.QueryRow(ctx, commitTxStatusSQLTemplate, ids, statues)
	duplicated, err := readInsertResult(ret, ids)
	if err != nil {
		return nil, fmt.Errorf("failed fetching results from query: %w", err)
	}
	if len(duplicated) == 0 {
		prometheusmetrics.Observe(db.metrics.databaseTxBatchCommitTxsStatusLatencySeconds, time.Since(start))
		return nil, nil
	}

	duplicatedTx := make([]txID, len(duplicated))
	for i, v := range duplicated {
		duplicatedTx[i] = txID(v)
	}
	prometheusmetrics.Observe(db.metrics.databaseTxBatchCommitTxsStatusLatencySeconds, time.Since(start))
	return duplicatedTx, nil
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

	// for every new namespace, we need to create a table.
	newNs, ok := nsToWrites[types.MetaNamespaceID]
	if !ok {
		return nil, nil
	}

	for _, ns := range newNs.keys {
		nsID, err := types.NamespaceIDFromBytes(ns)
		if err != nil {
			return nil, err
		}
		tableName := TableName(nsID)
		for _, stmt := range initStatementsWithTemplate {
			if _, err := tx.Exec(context.Background(), stmtFmt(stmt, tableName, db.name)); err != nil {
				return nil, err
			}
		}
	}

	return nil, nil
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

func (i *statesToBeCommitted) empty() bool {
	return i.updateWrites.empty() && i.newWrites.empty() && (i.batchStatus == nil || len(i.batchStatus.Status) == 0)
}

func (db *database) close() {
	db.pool.Close()
}

// readKeysAndVersions reads the keys and versions from the given rows.
func readKeysAndVersions(r pgx.Rows) ([][]byte, [][]byte, error) {
	var keys, versions [][]byte

	for r.Next() {
		var key, version []byte
		if err := r.Scan(&key, &version); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		versions = append(versions, version)
	}

	return keys, versions, r.Err()
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
