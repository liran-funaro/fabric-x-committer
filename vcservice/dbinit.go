package vcservice

import (
	"context"
	"fmt"
)

const createTableStmtTmpt = `
		CREATE TABLE IF NOT EXISTS %s (
			key bytea NOT NULL PRIMARY KEY,
			value bytea,
			version bytea
		);
	`

const createIndexStmtTmpt = `
		CREATE INDEX idx_%s ON %s(version);
	`

const validateFuncTmpt = `
		CREATE OR REPLACE FUNCTION validate_reads_%s(keys BYTEA[], versions BYTEA[])
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
				%s ON reads.keys = %s.key
			WHERE
				/* if the key does not exist in the committed state but read version is not null,
				we found a mismatch */
				(%s.key IS NULL AND reads.versions IS NOT NULL)
				OR
				/* if the key exists in the committed state but read version is null, we found a mismatch */
				(reads.versions IS NULL AND %s.key is NOT NULL)
				OR
				/* if the committed version of a key is different from the read version, we found a mismatch */
				reads.versions <> %s.version;
		END;
		$$
		LANGUAGE plpgsql;
	`

const commitFuncTmpt = `
		CREATE OR REPLACE FUNCTION commit_%s(_keys BYTEA[], _values BYTEA[], _versions BYTEA[])
		RETURNS VOID AS $$
		BEGIN
			INSERT INTO %s (key, value, version)
			SELECT _key, _value, _version
			FROM UNNEST(_keys, _values, _versions) AS t(_key, _value, _version)
			ON CONFLICT (key) DO UPDATE
			SET value = excluded.value, version = excluded.version;
		END;
		$$ LANGUAGE plpgsql;
	`

const dropTableStmtTmpt = "DROP TABLE IF EXISTS %s"

const dropValidateFuncStmtTmpt = "DROP FUNCTION IF EXISTS validate_reads_%s"

const dropCommitFuncStmtTmpt = "DROP FUNCTION IF EXISTS commit_%s"

// InitDatabase initialize the DB tables and methods.
func InitDatabase(config *DatabaseConfig, nsIDs []int) error {
	db, err := newDatabase(config)
	if err != nil {
		return err
	}
	defer db.close()

	ctx := context.Background()

	for _, nsID := range append(nsIDs, txIDsStatusNameSpace) {
		tableName := tableNameForNamespace(namespaceID(nsID))
		logger.Infof("Creating table '%s' and its methods.", tableName)

		statements := []string{
			fmt.Sprintf(createTableStmtTmpt, tableName),
			fmt.Sprintf(createIndexStmtTmpt, tableName, tableName),
			fmt.Sprintf(validateFuncTmpt, tableName, tableName, tableName, tableName, tableName, tableName),
			fmt.Sprintf(commitFuncTmpt, tableName, tableName),
		}

		for _, stmt := range statements {
			_, err = db.pool.Exec(ctx, stmt)
			if err != nil {
				panic(err)
			}
		}

		logger.Infof("Table '%s' is ready.", tableName)
	}

	return err
}
