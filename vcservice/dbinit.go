package vcservice

import (
	"context"
	"fmt"

	"github.com/yugabyte/pgx/v4/pgxpool"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
)

const createTxTableStmt = `
CREATE TABLE IF NOT EXISTS tx_status (
	tx_id bytea NOT NULL PRIMARY KEY,
	status integer
);
`

const commitTxStatus = `
CREATE OR REPLACE FUNCTION commit_tx_status(
	IN _tx_ids bytea[], IN _statues integer[], OUT result text, OUT violating bytea[]
)
    LANGUAGE plpgsql
AS $$
begin
	result = 'success';
    violating = NULL;
	INSERT INTO tx_status (tx_id, status)
	VALUES
		(unnest(_tx_ids), unnest(_statues));
exception
when unique_violation then
    violating = (
        SELECT array_agg(tx_id) FROM tx_status
        WHERE tx_id = ANY(_tx_ids)
    );
    if cardinality(violating) < cardinality(_tx_ids) then
        result = cardinality(violating) || '-unique-violation';
    else
        violating = NULL;
        result = 'all-unique-violation';
    end if;
end;$$;
`

const createTableStmtTemplate = `
CREATE TABLE IF NOT EXISTS %[1]s (
	key bytea NOT NULL PRIMARY KEY,
	value bytea DEFAULT NULL,
	version bytea DEFAULT '\x00'::bytea
);
`

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

// NewDatabasePool creates a new pool from a database config.
func NewDatabasePool(config *DatabaseConfig) (*pgxpool.Pool, error) {
	logger.Infof("DB source: %s", config.DataSourceName())
	poolConfig, err := pgxpool.ParseConfig(config.DataSourceName())
	if err != nil {
		return nil, fmt.Errorf("failed parsing datasource: %w", err)
	}

	poolConfig.MaxConns = config.MaxConnections
	poolConfig.MinConns = config.MinConnections

	pool, err := pgxpool.ConnectConfig(context.Background(), poolConfig)
	if err != nil {
		logger.Errorf("Failed making pool: %s", err)
		return nil, fmt.Errorf("failed making pool: %w", err)
	}
	logger.Debugf("DB pool created")
	return pool, nil
}

// InitDatabase initialize the DB tables and methods.
func InitDatabase(config *DatabaseConfig, nsIDs []int) error {
	pool, err := NewDatabasePool(config)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := initDatabaseTables(context.Background(), pool, nsIDs); err != nil {
		// TODO: handle error gracefully
		logger.Fatalf("Failed initialize database [%s] tables: %s", config.Database, err)
	}

	return nil
}

// ClearDatabase clears the DB tables and methods.
func ClearDatabase(config *DatabaseConfig, nsIDs []int) error {
	pool, err := NewDatabasePool(config)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := clearDatabaseTables(context.Background(), pool, nsIDs); err != nil {
		// TODO: handle error gracefully
		logger.Fatalf("Failed clearing database [%s] tables: %s", config.Database, err)
	}

	return nil
}

func initDatabaseTables(ctx context.Context, pool *pgxpool.Pool, nsIDs []int) error {
	logger.Info("Creating tx status table and its methods.")
	for _, stmt := range initStatements {
		err := yuga.ExecRetry(ctx, pool, stmt)
		if err != nil {
			return err
		}
	}
	logger.Info("Tx status table is ready.")

	for _, nsID := range nsIDs {
		tableName := NamespaceID(nsID).TableName()
		logger.Infof("Creating table '%s' and its methods.", tableName)

		for _, stmt := range initStatementsWithTemplate {
			err := yuga.ExecRetry(ctx, pool, fmt.Sprintf(stmt, tableName))
			if err != nil {
				return err
			}
		}

		logger.Infof("Table '%s' is ready.", tableName)
	}

	return nil
}

func clearDatabaseTables(ctx context.Context, pool *pgxpool.Pool, nsIDs []int) error {
	logger.Info("Dropping tx status table and its methods.")
	for _, stmt := range dropStatements {
		err := yuga.ExecRetry(ctx, pool, stmt)
		if err != nil {
			return err
		}
	}
	logger.Info("tx status table is cleared.")

	for _, nsID := range nsIDs {
		tableName := NamespaceID(nsID).TableName()
		logger.Infof("Dropping table '%s' and its methods.", tableName)

		for _, stmt := range dropStatementsWithTemplate {
			err := yuga.ExecRetry(ctx, pool, fmt.Sprintf(stmt, tableName))
			if err != nil {
				return err
			}
		}

		logger.Infof("Table '%s' is cleared.", tableName)
	}

	return nil
}
