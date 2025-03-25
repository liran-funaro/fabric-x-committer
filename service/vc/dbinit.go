package vc

import (
	"context"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/yugabyte/pgx/v4/pgxpool"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/vc/dbtest"
)

const createTxTableStmt = `
CREATE TABLE IF NOT EXISTS tx_status (
	tx_id bytea NOT NULL PRIMARY KEY,
	status integer,
  height bytea NOT NULL
) %[2]s;
`

const queryTxIDsStatus = `
SELECT tx_id, status, height
FROM tx_status
WHERE tx_id = ANY($1)
`

const (
	lastCommittedBlockNumberKey = "last committed block number"
)

const createMetadataTableStmt = `
CREATE TABLE IF NOT EXISTS metadata (
  key bytea NOT NULL PRIMARY KEY,
  value bytea
)
`

const initializeMetadataPrepStmt = `
INSERT INTO metadata VALUES ($1, $2) ON CONFLICT DO NOTHING;
`

const setMetadataPrepStmt = `
  UPDATE metadata SET value = $2 where key = $1;
`

const getMetadataPrepStmt = `
  SELECT value from metadata where key = $1;
`

const commitTxStatus = `
CREATE OR REPLACE FUNCTION commit_tx_status(
    IN _tx_ids bytea[], 
    IN _statuses integer[], 
    IN _heights bytea[], 
    OUT result text, 
    OUT violating bytea[]
)
    LANGUAGE plpgsql
AS $$
begin
    result = 'success';
    violating = NULL;

    INSERT INTO tx_status (tx_id, status, height)
    VALUES (unnest(_tx_ids), unnest(_statuses), unnest(_heights));

exception
    when unique_violation then
        violating = (
            SELECT array_agg(tx_id) 
            FROM tx_status
            WHERE tx_id = ANY(_tx_ids)
        );

        if cardinality(violating) < cardinality(_tx_ids) then
            result = cardinality(violating) || '-unique-violation';
        else
            violating = NULL;
            result = 'all-unique-violation';
        end if;
end;
$$;
`

const createTableStmtTemplate = `
CREATE TABLE IF NOT EXISTS %[1]s (
	key bytea NOT NULL PRIMARY KEY,
	value bytea DEFAULT NULL,
	version bytea DEFAULT '\x00'::bytea
) %[2]s;
`

const tableSplit = "SPLIT INTO 120 tablets"

// We avoid using index for now as it slows down inserts
// const createIndexStmtTemplate = `CREATE INDEX idx_%[1]s ON %[1]s(version);`

const validateFuncTemplate = `
CREATE OR REPLACE FUNCTION validate_reads_%[1]s(keys BYTEA[], versions BYTEA[])
RETURNS TABLE (key_mismatched BYTEA, version_mismatched BYTEA) AS
$$
BEGIN
	RETURN QUERY
	SELECT
		reads.keys AS key_mismatched,
		reads.versions AS version_mismatched
	FROM
		unnest(keys, versions) WITH ORDINALITY AS reads(keys, versions, ord_keys)
	LEFT JOIN
		%[1]s ON reads.keys = %[1]s.key
	WHERE
		/* if the key does not exist in the committed state but read version is not null,
		we found a mismatch */
		(%[1]s.key IS NULL AND reads.versions IS NOT NULL)
		OR
		/* if the key exists in the committed state but read version is null, we found a mismatch */
		(reads.versions IS NULL AND %[1]s.key is NOT NULL)
		OR
		/* if the committed version of a key is different from the read version, we found a mismatch */
		reads.versions <> %[1]s.version;
END;
$$
LANGUAGE plpgsql;
`

const commitUpdateFuncTemplate = `
CREATE OR REPLACE FUNCTION commit_update_%[1]s(_keys BYTEA[], _values BYTEA[], _versions BYTEA[])
RETURNS VOID AS $$
BEGIN
    UPDATE %[1]s
        SET value = t.value,
            version = t.version
    FROM (
        SELECT * FROM UNNEST(_keys, _values, _versions) AS t(key, value, version)
    ) AS t
    WHERE %[1]s.key = t.key;
END;
$$ LANGUAGE plpgsql;
`

const commitNewFuncTemplate = `
CREATE OR REPLACE FUNCTION commit_new_%[1]s(
	IN _keys bytea[], IN _values bytea[], OUT result text, OUT violating bytea[]
)
    LANGUAGE plpgsql
AS $$
begin
    result = 'success';
    violating = NULL;
    INSERT INTO %[1]s (key, value)
		SELECT k, v
		FROM UNNEST(_keys, _values) AS t(k, v);
exception
when unique_violation then
    violating = (
        SELECT array_agg(key) FROM %[1]s
        WHERE key = ANY(_keys)
    );
    if cardinality(violating) < cardinality(_keys) then
        result = cardinality(violating) || '-unique-violation';
    else
        violating = NULL;
        result = 'all-unique-violation';
    end if;
end;$$;
`

const (
	dropTableStmtTemplate            = "DROP TABLE IF EXISTS %[1]s"
	dropValidateFuncStmtTemplate     = "DROP FUNCTION IF EXISTS validate_reads_%[1]s"
	dropCommitUpdateFuncStmtTemplate = "DROP FUNCTION IF EXISTS commit_update_%[1]s"
	dropCommitNewFuncStmtTemplate    = "DROP FUNCTION IF EXISTS commit_new_%[1]s"
	dropTxStatusStmt                 = "DROP TABLE IF EXISTS tx_status"
	dropCommitTxStatusStmt           = "DROP FUNCTION IF EXISTS commit_tx_status"
)

var initStatements = []string{
	createTxTableStmt,
	commitTxStatus,
	createMetadataTableStmt,
}

var initStatementsWithTemplate = []string{
	createTableStmtTemplate,
	// createIndexStmtTemplate,
	validateFuncTemplate,
	commitUpdateFuncTemplate,
	commitNewFuncTemplate,
}

var dropStatements = []string{
	dropTxStatusStmt,
	dropCommitTxStatusStmt,
}

var dropStatementsWithTemplate = []string{
	dropTableStmtTemplate,
	dropValidateFuncStmtTemplate,
	dropCommitUpdateFuncStmtTemplate,
	dropCommitNewFuncStmtTemplate,
}

var systemNamespaces = []string{
	types.MetaNamespaceID, types.ConfigNamespaceID,
}

// NewDatabasePool creates a new pool from a database config.
func NewDatabasePool(ctx context.Context, config *DatabaseConfig) (*pgxpool.Pool, error) {
	logger.Infof("DB source: %s", config.DataSourceName())
	poolConfig, err := pgxpool.ParseConfig(config.DataSourceName())
	if err != nil {
		return nil, fmt.Errorf("failed parsing datasource: %w", err)
	}

	poolConfig.MaxConns = config.MaxConnections
	poolConfig.MinConns = config.MinConnections

	var pool *pgxpool.Pool
	if retryErr := config.Retry.Execute(ctx, func() error {
		pool, err = pgxpool.ConnectConfig(ctx, poolConfig)
		return err
	}); retryErr != nil {
		logger.Errorf("Failed making pool: %s", retryErr)
		return nil, fmt.Errorf("failed making pool: %w", retryErr)
	}

	logger.Debugf("DB pool created")
	return pool, nil
}

// ClearDatabase clears the DB tables and methods.
func ClearDatabase(ctx context.Context, config *DatabaseConfig, nsIDs []string) error {
	pool, err := NewDatabasePool(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err = clearDatabaseTables(ctx, pool, nsIDs); err != nil {
		return errors.Wrapf(err, "failed clearing database [%s] tables: %s", config.Database)
	}

	return nil
}

func getDbType(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	rows, err := pool.Query(ctx, "SELECT version();")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	for rows.Next() {
		var out string
		err = rows.Scan(&out)
		if err != nil {
			return "", err
		}
		out = strings.ToLower(out)
		if strings.Contains(out, "yugabyte") {
			return "yugabyte", nil
		} else if strings.Contains(out, "postgresql") {
			return "postgresql", nil
		}
	}
	return "", nil
}

func stmtFmt(stmtTemplate, tableName, dbType string) string {
	splitStmt := ""
	if dbType == "yugabyte" {
		splitStmt = tableSplit
	}
	// We add a fake template at the end, so we always consume both parameters.
	return fmt.Sprintf(stmtTemplate+"%.0[1]s%.0[2]s", tableName, splitStmt)
}

func initDatabaseTables(ctx context.Context, pool *pgxpool.Pool, nsIDs []string) error {
	logger.Infof("Starting DB tables initialization. Already performed action will be ignored.")
	dbType, err := getDbType(ctx, pool)
	if err != nil {
		return err
	}
	logger.Infof("DBType: %v", dbType)

	for _, stmt := range initStatements {
		if execErr := dbtest.PoolExecOperation(ctx, pool, stmtFmt(stmt, "", dbType)); execErr != nil {
			return fmt.Errorf("failed initializing tables: %w", execErr)
		}
	}
	logger.Info("Created tx status table, metadata table, and its methods.")
	if execErr := dbtest.PoolExecOperation(ctx,
		pool,
		stmtFmt(initializeMetadataPrepStmt, "", dbType),
		[]byte(lastCommittedBlockNumberKey),
		nil,
	); execErr != nil {
		return fmt.Errorf("failed initialization metadata table: %w", execErr)
	}

	nsIDs = append(nsIDs, systemNamespaces...)
	for _, nsID := range nsIDs {
		tableName := TableName(nsID)
		for _, stmt := range initStatementsWithTemplate {
			if execErr := dbtest.PoolExecOperation(ctx, pool, stmtFmt(stmt, tableName, dbType)); execErr != nil {
				return fmt.Errorf("failed creating meta-namespace: %w", execErr)
			}
			logger.Infof("Created table '%s' and its methods.", tableName)
		}
	}
	return nil
}

func clearDatabaseTables(ctx context.Context, pool *pgxpool.Pool, nsIDs []string) error {
	logger.Info("Dropping tx status table and its methods.")
	dbType, err := getDbType(ctx, pool)
	if err != nil {
		return err
	}
	logger.Infof("DBType: %v", dbType)

	for _, stmt := range dropStatements {
		if execErr := dbtest.PoolExecOperation(ctx,
			pool,
			stmtFmt(stmt, "", dbType),
		); execErr != nil {
			return fmt.Errorf("failed clearing database tables: %w", execErr)
		}
	}
	logger.Info("tx status table is cleared.")

	nsIDs = append(nsIDs, systemNamespaces...)
	for _, nsID := range nsIDs {
		tableName := TableName(nsID)
		logger.Infof("Dropping table '%s' and its methods.", tableName)

		for _, stmt := range dropStatementsWithTemplate {
			if execErr := dbtest.PoolExecOperation(ctx, pool, stmtFmt(stmt, tableName, dbType)); execErr != nil {
				return fmt.Errorf("failed clearing database tables: %w", execErr)
			}
		}

		logger.Infof("Table '%s' is cleared.", tableName)
	}

	return nil
}
