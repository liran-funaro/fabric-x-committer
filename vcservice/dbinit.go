package vcservice

import (
	"context"
	"fmt"
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
		SELECT _id, _status
		FROM UNNEST(_tx_ids, _statues) AS t(_id, _status);
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
	INSERT INTO %[1]s (key, value, version)
	SELECT _key, _value, _version
	FROM UNNEST(_keys, _values, _versions) AS t(_key, _value, _version)
		ON CONFLICT (key) DO UPDATE
		SET value = excluded.value, version = excluded.version;
END;
$$ LANGUAGE plpgsql;
`

const commitNewWithValFuncTemplate = `
CREATE OR REPLACE FUNCTION commit_new_with_val_%[1]s(
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

const commitNewFuncWithoutValueTemplate = `
CREATE OR REPLACE FUNCTION commit_new_without_val_%[1]s(
	IN _keys bytea[], OUT result text, OUT violating bytea[]
)
    LANGUAGE plpgsql
AS $$
begin
    result = 'success';
    violating = NULL;
    insert into %[1]s values (UNNEST(_keys));
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
	dropTableStmtTemplate                = "DROP TABLE IF EXISTS %[1]s"
	dropValidateFuncStmtTemplate         = "DROP FUNCTION IF EXISTS validate_reads_%[1]s"
	dropCommitFuncStmtTemplate           = "DROP FUNCTION IF EXISTS commit_%[1]s"
	dropCommitWithValFuncStmtTemplate    = "DROP FUNCTION IF EXISTS commit_new_with_val_%[1]s"
	dropCommitWithoutValFuncStmtTemplate = "DROP FUNCTION IF EXISTS commit_new_without_val_%[1]s"
	dropTxStatusStmt                     = "DROP TABLE IF EXISTS tx_status"
	dropCommitTxStatusStmt               = "DROP FUNCTION IF EXISTS commit_tx_status"
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
	commitNewWithValFuncTemplate,
	commitNewFuncWithoutValueTemplate,
}

var dropStatements = []string{
	dropTxStatusStmt,
	dropCommitTxStatusStmt,
}

var dropStatementsWithTemplate = []string{
	dropTableStmtTemplate,
	dropValidateFuncStmtTemplate,
	dropCommitFuncStmtTemplate,
	dropCommitWithValFuncStmtTemplate,
	dropCommitWithoutValFuncStmtTemplate,
}

// InitDatabase initialize the DB tables and methods.
func InitDatabase(config *DatabaseConfig, nsIDs []int) error {
	db, err := newDatabase(config, nil)
	if err != nil {
		return err
	}
	defer db.close()

	if err := initDatabaseTables(db, nsIDs); err != nil {
		// TODO: handle error gracefully
		panic(err)
	}

	return nil
}

func initDatabaseTables(db *database, nsIDs []int) error {
	ctx := context.Background()

	logger.Info("Creating tx status table and its methods.")
	for _, stmt := range initStatements {
		_, err := db.pool.Exec(ctx, stmt)
		if err != nil {
			return err
		}
	}
	logger.Info("Tx status table is ready.")

	for _, nsID := range nsIDs {
		tableName := tableNameForNamespace(namespaceID(nsID))
		logger.Infof("Creating table '%s' and its methods.", tableName)

		for _, stmt := range initStatementsWithTemplate {
			_, err := db.pool.Exec(ctx, fmt.Sprintf(stmt, tableName))
			if err != nil {
				return err
			}
		}

		logger.Infof("Table '%s' is ready.", tableName)
	}

	return nil
}

func clearDatabaseTables(db *database, nsIDs []int) error {
	ctx := context.Background()

	logger.Info("Dropping tx status table and its methods.")
	for _, stmt := range dropStatements {
		_, err := db.pool.Exec(ctx, stmt)
		if err != nil {
			return err
		}
	}
	logger.Info("tx status table is cleared.")

	for _, nsID := range nsIDs {
		tableName := tableNameForNamespace(namespaceID(nsID))
		logger.Infof("Dropping table '%s' and its methods.", tableName)

		for _, stmt := range dropStatementsWithTemplate {
			_, err := db.pool.Exec(ctx, fmt.Sprintf(stmt, tableName))
			if err != nil {
				return err
			}
		}

		logger.Infof("Table '%s' is cleared.", tableName)
	}

	return nil
}
