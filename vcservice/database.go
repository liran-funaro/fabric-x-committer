package vcservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/yugabyte/pgx/v4"
	"github.com/yugabyte/pgx/v4/pgxpool"
)

const (
	// tableNameTemplate is the template for the table name for each namespace.
	tableNameTemplate = "ns_%d"

	// validateReadsSQLTemplate is the template for validating reads on each namespace.
	validateReadsSQLTemplate = "SELECT * FROM validate_reads_ns_%d($1::varchar[], $2::bytea[])"
	// queryVersionsSQLTemplate is the template for the querying versions for given keys on each namespace.
	queryVersionsSQLTemplate = "SELECT key, version FROM %s WHERE key = ANY($1)"
	// commitWritesSQLTemplate is the template for the committing writes on each namespace.
	commitWritesSQLTemplate = "SELECT commit_ns_%d($1::varchar[], $2::bytea[], $3::bytea[])"
)

type (
	// database handles the database operations.
	database struct {
		pool *pgxpool.Pool
	}

	// keyToVersion is a map from key to version.
	keyToVersion map[string][]byte
)

// newDatabase creates a new database.
func newDatabase(config *DatabaseConfig) (*database, error) {
	poolConfig, err := pgxpool.ParseConfig(config.DataSourceName())
	if err != nil {
		return nil, err
	}

	poolConfig.MaxConns = config.MaxConnections
	poolConfig.MinConns = config.MinConnections

	pool, err := pgxpool.ConnectConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, err
	}

	return &database{
		pool: pool,
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
		return nil, err
	}
	defer mismatch.Close()

	keys, values, err := readKeysAndVersions(mismatch)
	if err != nil {
		return nil, err
	}

	mismatchingReads := &reads{}
	mismatchingReads.appendMany(keys, values)

	return mismatchingReads, nil
}

// queryVersionsIfPresent queries the versions for the given keys if they exist.
func (db *database) queryVersionsIfPresent(nsID namespaceID, queryKeys []string) (keyToVersion, error) {
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
		kToV[key] = foundVersions[i]
	}

	return kToV, nil
}

// commit commits the writes to the database.
func (db *database) commit(nsToWrites namespaceToWrites) error {
	// we want to commit all the writes to all namespaces or none at all
	// so we use a database transaction. Otherwise, the failure and recovery
	// logic will be very complicated.

	ctx := context.Background()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}

	// This will be executed if an error occurs. If transaction is committed, this will be a no-op.
	defer func() {
		err = tx.Rollback(ctx)
		if !errors.Is(err, pgx.ErrTxClosed) {
			logger.Warn("error rolling-back transaction: ", err)
		}
	}()

	for nsID, writes := range nsToWrites {
		query := fmt.Sprintf(commitWritesSQLTemplate, nsID)

		_, err := tx.Exec(ctx, query, writes.keys, writes.values, writes.versions)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (db *database) close() {
	db.pool.Close()
}

// readKeysAndVersions reads the keys and versions from the given rows.
func readKeysAndVersions(r pgx.Rows) ([]string, [][]byte, error) {
	var keys []string
	var versions [][]byte

	for r.Next() {
		var key string
		var version []byte
		if err := r.Scan(&key, &version); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		versions = append(versions, version)
	}

	return keys, versions, r.Err()
}

// tableNameForNamespace returns the table name for the given namespace.
func tableNameForNamespace(nsID namespaceID) string {
	return fmt.Sprintf(tableNameTemplate, nsID)
}
