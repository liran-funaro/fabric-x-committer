package vcservice

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v4"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
)

// TODO: all the statement templates will be moved to a different package once we decide on the
//	     chaincode deployment model.

const (
	createTableStmtTmpt = `
		CREATE TABLE IF NOT EXISTS %s (
			key varchar NOT NULL PRIMARY KEY,
			value bytea NOT NULL,
			version bytea
		);
	`

	createIndexStmtTmpt = `
		CREATE INDEX idx_%s ON %s(version);
	`

	validateFuncTmpt = `
		CREATE OR REPLACE FUNCTION validate_reads_%s(keys VARCHAR[], versions BYTEA[])
		RETURNS TABLE (key_mismatched VARCHAR, version_mismatched BYTEA) AS
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

	commitFuncTmpt = `
		CREATE OR REPLACE FUNCTION commit_%s(_keys VARCHAR[], _values BYTEA[], _versions BYTEA[])
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

	dropTableStmtTmpt        = "DROP TABLE IF EXISTS %s"
	dropValidateFuncStmtTmpt = "DROP FUNCTION IF EXISTS validate_reads_%s"
	dropCommitFuncStmtTmpt   = "DROP FUNCTION IF EXISTS commit_%s"

	queryKeyValueVersionSQLTmpt = "SELECT key, value, version FROM %s WHERE key = ANY($1)"

	ns1 = namespaceID(1)
	ns2 = namespaceID(2)
)

var (
	v0 = versionNumber(0).bytes()
	v1 = versionNumber(1).bytes()
)

type databaseTestEnv struct {
	db       *database
	dbRunner *runner.YugabyteDB
}

func newDatabaseTestEnv(t *testing.T) *databaseTestEnv {
	dbRunner := &runner.YugabyteDB{}
	require.NoError(t, dbRunner.Start())

	psqlInfo := dbRunner.ConnectionSettings().DataSourceName()
	conn, err := pgx.Connect(context.Background(), psqlInfo)
	require.NoError(t, err)

	t.Cleanup(func() {
		assert.NoError(t, conn.Close(context.Background()))
		assert.NoError(t, dbRunner.Stop())
	})

	return &databaseTestEnv{
		db:       newDatabase(conn),
		dbRunner: dbRunner,
	}
}

func TestValidateNamespaceReads(t *testing.T) {
	env := newDatabaseTestEnv(t)

	env.populateDataWithCleanup(
		t,
		[]namespaceID{ns1, ns2},
		namespaceToWrites{
			ns1: {
				keys:     []string{"key1", "key2", "key3"},
				values:   [][]byte{[]byte("value1"), []byte("value2"), []byte("value3")},
				versions: [][]byte{v0, v0, v0},
			},
			ns2: {
				keys:     []string{"key4", "key5", "key6"},
				values:   [][]byte{[]byte("value4"), []byte("value5"), []byte("value6")},
				versions: [][]byte{v1, v1, v1},
			},
		},
	)

	tests := []struct {
		name                    string
		nsID                    namespaceID
		r                       *reads
		expectedMismatchedReads *reads
	}{
		{
			name:                    "empty reads",
			nsID:                    ns1,
			r:                       &reads{},
			expectedMismatchedReads: &reads{},
		},
		{
			name: "reads of only non-existing keys and all matching versions",
			nsID: ns1,
			r: &reads{
				keys:     []string{"key4", "key5", "key6"},
				versions: [][]byte{nil, nil, nil},
			},
			expectedMismatchedReads: &reads{},
		},
		{
			name: "reads of only non-existing keys and some mismatching versions",
			nsID: ns2,
			r: &reads{
				keys:     []string{"key7", "key8", "key9"},
				versions: [][]byte{nil, v0, nil},
			},
			expectedMismatchedReads: &reads{
				keys:     []string{"key8"},
				versions: [][]byte{v0},
			},
		},
		{
			name: "reads of only non-existing keys and all mismatching versions",
			nsID: ns2,
			r: &reads{
				keys:     []string{"key7", "key8", "key9"},
				versions: [][]byte{v1, v0, v1},
			},
			expectedMismatchedReads: &reads{
				keys:     []string{"key7", "key8", "key9"},
				versions: [][]byte{v1, v0, v1},
			},
		},
		{
			name: "reads of existing keys and all matching versions",
			nsID: ns1,
			r: &reads{
				keys:     []string{"key1", "key2", "key3"},
				versions: [][]byte{v0, v0, v0},
			},
			expectedMismatchedReads: &reads{},
		},
		{
			name: "reads of existing keys and some mismatching versions",
			nsID: ns1,
			r: &reads{
				keys:     []string{"key1", "key2", "key3"},
				versions: [][]byte{v1, v0, v1},
			},
			expectedMismatchedReads: &reads{
				keys:     []string{"key1", "key3"},
				versions: [][]byte{v1, v1},
			},
		},
		{
			name: "reads of existing keys and all mismatching versions",
			nsID: ns2,
			r: &reads{
				keys:     []string{"key4", "key5", "key6"},
				versions: [][]byte{v0, v0, v0},
			},
			expectedMismatchedReads: &reads{
				keys:     []string{"key4", "key5", "key6"},
				versions: [][]byte{v0, v0, v0},
			},
		},
		{
			name: "reads of existing and non-existing keys and some mismatching versions",
			nsID: ns2,
			r: &reads{
				keys:     []string{"key4", "key5", "key6", "key7", "key8", "key9"},
				versions: [][]byte{v1, v0, v1, nil, v0, nil},
			},
			expectedMismatchedReads: &reads{
				keys:     []string{"key5", "key8"},
				versions: [][]byte{v0, v0},
			},
		},
	}

	for _, test := range tests {
		tt := test
		t.Run(tt.name, func(t *testing.T) {
			// TODO: fix issue #241 to enable parallel text

			mismatchingReads, err := env.db.validateNamespaceReads(tt.nsID, tt.r)
			require.NoError(t, err)
			require.Equal(t, tt.expectedMismatchedReads, mismatchingReads)
		})
	}
}

func TestDBCommit(t *testing.T) {
	dbEnv := newDatabaseTestEnv(t)

	dbEnv.populateDataWithCleanup(
		t,
		[]namespaceID{ns1, ns2},
		namespaceToWrites{},
	)

	nsToWrites := namespaceToWrites{
		ns1: {
			keys:     []string{"key1", "key2", "key3"},
			values:   [][]byte{[]byte("value1"), []byte("value2"), []byte("value3")},
			versions: [][]byte{v0, v0, v0},
		},
		ns2: {
			keys:     []string{"key4", "key5", "key6"},
			values:   [][]byte{[]byte("value4"), []byte("value5"), []byte("value6")},
			versions: [][]byte{v1, v1, v1},
		},
	}

	require.NoError(t, dbEnv.db.commit(nsToWrites))
	dbEnv.rowExists(t, ns1, *nsToWrites[ns1])
	dbEnv.rowExists(t, ns2, *nsToWrites[ns2])
}

func (env *databaseTestEnv) populateDataWithCleanup(t *testing.T, nsIDs []namespaceID, writes namespaceToWrites) {
	ctx := context.Background()
	for _, nsID := range nsIDs {
		tableName := tableNameForNamespace(nsID)

		statements := []string{
			fmt.Sprintf(createTableStmtTmpt, tableName),
			fmt.Sprintf(createIndexStmtTmpt, tableName, tableName),
			fmt.Sprintf(validateFuncTmpt, tableName, tableName, tableName, tableName, tableName, tableName),
			fmt.Sprintf(commitFuncTmpt, tableName, tableName),
		}

		for _, stmt := range statements {
			_, err := env.db.conn.Exec(ctx, stmt)
			require.NoError(t, err)
		}
	}

	require.NoError(t, env.db.commit(writes))

	t.Cleanup(func() {
		dropStmtTmpts := []string{
			dropTableStmtTmpt,
			dropValidateFuncStmtTmpt,
			dropCommitFuncStmtTmpt,
		}

		ctx := context.Background()
		for _, nsID := range nsIDs {
			tableName := tableNameForNamespace(nsID)

			for _, dropStmtTmpt := range dropStmtTmpts {
				_, err := env.db.conn.Exec(ctx, fmt.Sprintf(dropStmtTmpt, tableName))
				require.NoError(t, err)
			}
		}
	})
}

func (env *databaseTestEnv) rowExists(t *testing.T, nsID namespaceID, expectedRows namespaceWrites) {
	query := fmt.Sprintf(queryKeyValueVersionSQLTmpt, tableNameForNamespace(nsID))

	kvPairs, err := env.db.conn.Query(context.Background(), query, expectedRows.keys)
	require.NoError(t, err)
	defer kvPairs.Close()

	type valueVersion struct {
		value   []byte
		version []byte
	}

	actualRows := map[string]*valueVersion{}

	for kvPairs.Next() {
		var key string
		vv := &valueVersion{}

		require.NoError(t, kvPairs.Scan(&key, &vv.value, &vv.version))
		actualRows[key] = vv
	}

	require.NoError(t, kvPairs.Err())
	require.Equal(t, len(expectedRows.keys), len(actualRows))
	for i, key := range expectedRows.keys {
		require.Equal(t, expectedRows.values[i], actualRows[key].value)
		require.Equal(t, expectedRows.versions[i], actualRows[key].version)
	}
}
