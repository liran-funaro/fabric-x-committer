package vcservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgtype"
	"github.com/yugabyte/pgx/v4"
	"github.com/yugabyte/pgx/v4/pgxpool"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
)

const (
	// tableNameTemplate is the template for the table name for each namespace.
	tableNameTemplate = "ns_%d"

	// validateReadsSQLTemplate is the template for validating reads on each namespace.
	validateReadsSQLTemplate = "SELECT * FROM validate_reads_ns_%d($1::bytea[], $2::bytea[]);"
	// queryVersionsSQLTemplate is the template for the querying versions for given keys on each namespace.
	queryVersionsSQLTemplate = "SELECT key, version FROM %s WHERE key = ANY($1);"
	// commitWritesSQLTemplate is the template for the committing writes on each namespace.
	commitTxStatusSQLTemplate            = "SELECT commit_tx_status($1::bytea[], $2::integer[]);"
	commitWritesSQLTemplate              = "SELECT commit_update_ns_%d($1::bytea[], $2::bytea[], $3::bytea[]);"
	commitNewWithValWritesSQLTemplate    = "SELECT commit_new_with_val_ns_%d($1::bytea[], $2::bytea[]);"
	commitNewWithoutValWritesSQLTemplate = "SELECT commit_new_without_val_ns_%d($1::bytea[]);"
)

type (
	// database handles the database operations.
	database struct {
		pool    *pgxpool.Pool
		metrics *perfMetrics
	}

	// keyToVersion is a map from key to version.
	keyToVersion map[string][]byte
)

// newDatabase creates a new database.
func newDatabase(config *DatabaseConfig, metrics *perfMetrics) (*database, error) {
	logger.Debugf("DB source: %s", config.DataSourceName())
	poolConfig, err := pgxpool.ParseConfig(config.DataSourceName())
	if err != nil {
		return nil, err
	}

	poolConfig.MaxConns = config.MaxConnections
	poolConfig.MinConns = config.MinConnections

	pool, err := pgxpool.ConnectConfig(context.Background(), poolConfig)
	if err != nil {
		logger.Errorf("Failed making pool: %s", err)
		return nil, err
	}

	if metrics == nil {
		metrics = newVCServiceMetrics(false)
	}

	return &database{
		pool:    pool,
		metrics: metrics,
	}, nil
}

// validateNamespaceReads validates the reads for a given namespace.
func (db *database) validateNamespaceReads(nsID namespaceID, r *reads) (*reads /* mismatching reads */, error) {
	// For each namespace nsID, we use the validate_reads_ns_<nsID> function to validate
	// the reads. This function returns the keys and versions of the mismatching reads.
	// Note that we have a table per namespace.
	// We have a validate function per namespace so that we can use the static SQL
	// to avoid parsing, planning and optimizing the query for each invoke. If we use
	// a common function for all namespace, we need to pass the table name as a parameter
	// which makes the query dynamic and hence we lose the benefits of static SQL.
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

	return mismatchingReads, nil
}

// queryVersionsIfPresent queries the versions for the given keys if they exist.
func (db *database) queryVersionsIfPresent(nsID namespaceID, queryKeys [][]byte) (keyToVersion, error) {
	query := fmt.Sprintf(queryVersionsSQLTemplate, tableNameForNamespace(nsID))
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

	return kToV, nil
}

func (db *database) execCommitUpdate(ctx context.Context, tx pgx.Tx, nsToWrites namespaceToWrites) error {
	for nsID, writes := range nsToWrites {
		if writes.empty() {
			continue
		}

		query := fmt.Sprintf(commitWritesSQLTemplate, nsID)
		t := db.metrics.newLatencyTimer(fmt.Sprintf("db.execCommitUpdate.tx.exec.%d", nsID))
		_, err := tx.Exec(ctx, query, writes.keys, writes.values, writes.versions)
		t.observe()
		if err != nil {
			return fmt.Errorf("failed tx exec: %w", err)
		}
	}

	return nil
}

func (db *database) execCommitNew( // nolint:revive
	ctx context.Context, tx pgx.Tx, nsToWrites namespaceToWrites, withVal bool,
) (namespaceToReads /* mismatched */, error) {
	mismatch := make(namespaceToReads)
	var queryTemplate string
	var metricTemplate string
	if withVal {
		queryTemplate = commitNewWithValWritesSQLTemplate
		metricTemplate = "db.tx.execCommitNew_WithVal.query.%d"
	} else {
		queryTemplate = commitNewWithoutValWritesSQLTemplate
		metricTemplate = "db.tx.execCommitNew_WithoutVal.query.%d"
	}

	for nsID, writes := range nsToWrites {
		if writes.empty() {
			continue
		}

		args := []any{writes.keys}
		if withVal {
			args = append(args, writes.values)
		}
		t := db.metrics.newLatencyTimer(fmt.Sprintf(metricTemplate, nsID))
		ret := tx.QueryRow(ctx, fmt.Sprintf(queryTemplate, nsID), args...)
		violating, err := readInsertResult(ret, writes.keys)
		t.observe()
		if err != nil {
			return nil, fmt.Errorf("failed fetching results from query: %w", err)
		}

		if len(violating) > 0 {
			// We can use arbitrary versions from the list of writes since they are all 'nil'.
			mismatch.getOrCreate(nsID).appendMany(violating, writes.versions[:len(violating)])
		}
	}

	return mismatch, nil
}

func (db *database) execCommitTxStatus(
	ctx context.Context, tx pgx.Tx, batchStatus *protovcservice.TransactionStatus,
) ([]TxID /* duplicated */, error) {
	if batchStatus == nil || len(batchStatus.Status) == 0 {
		return nil, nil
	}

	ids := make([][]byte, 0, len(batchStatus.Status))
	statues := make([]int, 0, len(batchStatus.Status))
	for txID, status := range batchStatus.Status {
		ids = append(ids, []byte(txID))
		statues = append(statues, int(status))
	}

	t := db.metrics.newLatencyTimer("db.execCommitTxStatus.tx.query")
	ret := tx.QueryRow(ctx, commitTxStatusSQLTemplate, ids, statues)
	duplicated, err := readInsertResult(ret, ids)
	t.observe()
	if err != nil {
		return nil, fmt.Errorf("failed fetching results from query: %w", err)
	}
	if len(duplicated) == 0 {
		return nil, nil
	}

	duplicatedTx := make([]TxID, len(duplicated))
	for i, v := range duplicated {
		duplicatedTx[i] = TxID(v)
	}
	return duplicatedTx, nil
}

type commitInfo struct {
	updateWrites        namespaceToWrites
	newWithoutValWrites namespaceToWrites
	newWithValWrites    namespaceToWrites
	batchStatus         *protovcservice.TransactionStatus
}

func (i *commitInfo) empty() bool {
	return i.updateWrites.empty() && i.newWithoutValWrites.empty() && i.newWithValWrites.empty() &&
		(i.batchStatus == nil || len(i.batchStatus.Status) == 0)
}

func (db *database) commitInner(
	ctx context.Context,
	tx pgx.Tx,
	info *commitInfo,
) (namespaceToReads, []TxID, error) {
	duplicated, err := db.execCommitTxStatus(ctx, tx, info.batchStatus)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx execCommitTxStatus: %w", err)
	}

	if err = db.execCommitUpdate(ctx, tx, info.updateWrites); err != nil {
		return nil, nil, fmt.Errorf("failed tx execCommitUpdate: %w", err)
	}

	mismatched, err := db.execCommitNew(ctx, tx, info.newWithoutValWrites, false)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx execCommitNewWithoutVal: %w", err)
	}

	mismatchedWithVal, err := db.execCommitNew(ctx, tx, info.newWithValWrites, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx execCommitNewWithVal: %w", err)
	}
	mismatched = mismatched.merge(mismatchedWithVal)

	return mismatched, duplicated, nil
}

// commit commits the writes to the database.
func (db *database) commit(info *commitInfo) (namespaceToReads, []TxID, error) {
	if info.empty() {
		return nil, nil, nil
	}

	// We want to commit all the writes to all namespaces or none at all,
	// so we use a database transaction. Otherwise, the failure and recovery
	// logic will be very complicated.
	ctx := context.Background()
	t := db.metrics.newLatencyTimer("db.tx.begin")
	tx, err := db.pool.Begin(ctx)
	t.observe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx begin: %w", err)
	}

	// This will be executed if an error occurs. If transaction is committed, this will be a no-op.
	defer func() {
		rollbackErr := tx.Rollback(ctx)
		if !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed rolling-back transaction: ", rollbackErr)
		}
	}()

	mismatched, duplicated, err := db.commitInner(ctx, tx, info)
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx commitInner: %w", err)
	}
	if !mismatched.empty() || len(duplicated) > 0 {
		// rollback
		return mismatched, duplicated, nil
	}

	t = db.metrics.newLatencyTimer("db.tx.commit")
	err = tx.Commit(ctx)
	t.observe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed tx commit: %w", err)
	}

	return nil, nil, nil
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

// tableNameForNamespace returns the table name for the given namespace.
func tableNameForNamespace(nsID namespaceID) string {
	return fmt.Sprintf(tableNameTemplate, nsID)
}
