package vcservice

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v4"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
)

type validatorTestEnv struct {
	v            *transactionValidator
	preparedTxs  chan *preparedTransactions
	validatedTxs chan *validatedTransactions
}

func newValidatorTestEnv(t *testing.T) *validatorTestEnv {
	db := runner.YugabyteDB{}
	require.NoError(t, db.Start())

	preparedTxs := make(chan *preparedTransactions, 10)
	validatedTxs := make(chan *validatedTransactions, 10)

	psqlInfo := db.ConnectionSettings().DataSourceName()
	conn, err := pgx.Connect(context.Background(), psqlInfo)
	require.NoError(t, err)

	v := newValidator(conn, preparedTxs, validatedTxs)

	t.Cleanup(func() {
		close(preparedTxs)
		close(validatedTxs)
		_ = db.Stop()
		_ = conn.Close(context.Background())
	})

	return &validatorTestEnv{
		v:            v,
		preparedTxs:  preparedTxs,
		validatedTxs: validatedTxs,
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	env := newValidatorTestEnv(t)
	env.v.start(1)

	v0 := versionNumber(0).bytes()
	v1 := versionNumber(1).bytes()
	v2 := versionNumber(2).bytes()

	populateDataWithCleanup(
		t,
		env.v.databaseConnection,
		[]namespaceID{1, 2},
		namespaceToWrites{
			1: {
				keys:     []string{"key1.1", "key1.2", "key1.3", "key1.4"},
				values:   [][]byte{[]byte("value1.1"), []byte("value1.2"), []byte("value1.3"), []byte("value1.4")},
				versions: [][]byte{v1, v1, v2, v2},
			},
			2: {
				keys:     []string{"key2.1", "key2.2", "key2.3", "key2.4"},
				values:   [][]byte{[]byte("value2.1"), []byte("value2.2"), []byte("value2.3"), []byte("value2.4")},
				versions: [][]byte{v0, v0, v1, v1},
			},
		},
	)

	tx1NonBlindWrites := namespaceToWrites{
		1: {
			keys:     []string{"key1.1"},
			values:   [][]byte{[]byte("value1.1.1")},
			versions: [][]byte{v2},
		},
		2: {
			keys:     []string{"key2.1"},
			values:   [][]byte{[]byte("value1.1.1")},
			versions: [][]byte{v2},
		},
	}
	tx2NonBlindWrites := namespaceToWrites{
		1: {
			keys:     []string{"key1.5"},
			values:   [][]byte{[]byte("value1.5.1")},
			versions: [][]byte{v0},
		},
	}
	tx3NonBlindWrites := namespaceToWrites{
		2: {
			keys:     []string{"key2.2"},
			values:   [][]byte{[]byte("value2.2.1")},
			versions: [][]byte{v2},
		},
	}
	tx3BlindWrites := namespaceToWrites{
		1: {
			keys:     []string{"key1.6"},
			values:   [][]byte{[]byte("value1.6")},
			versions: [][]byte{nil},
		},
	}

	// Note: the order of the sub-test is important
	tests := []struct {
		name                string
		preparedTx          *preparedTransactions
		expectedValidatedTx *validatedTransactions
	}{
		{
			name: "all valid tx",
			preparedTx: &preparedTransactions{
				namespaceToReadEntries: namespaceToReads{
					1: &reads{
						keys:     []string{"key1.1", "key1.2", "key1.5"},
						versions: [][]byte{v1, v1, nil},
					},
					2: &reads{
						keys:     []string{"key2.1", "key2.2", "key2.5"},
						versions: [][]byte{v0, v0, nil},
					},
				},
				readToTransactionIndices: readToTransactions{
					comparableRead{1, "key1.1", string(v1)}: []TxID{"tx1"},
					comparableRead{1, "key1.2", string(v1)}: []TxID{"tx1"},
					comparableRead{1, "key1.5", ""}:         []TxID{"tx2"},
					comparableRead{2, "key2.1", string(v0)}: []TxID{"tx1"},
					comparableRead{2, "key2.2", string(v0)}: []TxID{"tx3"},
					comparableRead{2, "key2.5", ""}:         []TxID{"tx3"},
				},
				nonBlindWritesPerTransaction: transactionToWrites{
					"tx1": tx1NonBlindWrites,
					"tx2": tx2NonBlindWrites,
					"tx3": tx3NonBlindWrites,
				},
				blindWritesPerTransaction: transactionToWrites{
					"tx3": tx3BlindWrites,
				},
			},
			expectedValidatedTx: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx1": tx1NonBlindWrites,
					"tx2": tx2NonBlindWrites,
					"tx3": tx3NonBlindWrites,
				},
				validTxBlindWrites: transactionToWrites{
					"tx3": tx3BlindWrites,
				},
				invalidTxIndices: map[TxID]bool{},
			},
		},
		{
			name: "all invalid tx",
			preparedTx: &preparedTransactions{
				namespaceToReadEntries: namespaceToReads{
					1: &reads{
						keys:     []string{"key1.1", "key1.2", "key1.5"},
						versions: [][]byte{v0, v0, v1},
					},
					2: &reads{
						keys:     []string{"key2.1", "key2.2", "key2.5"},
						versions: [][]byte{nil, nil, nil},
					},
				},
				readToTransactionIndices: readToTransactions{
					comparableRead{1, "key1.1", string(v0)}: []TxID{"tx1"},
					comparableRead{1, "key1.2", string(v0)}: []TxID{"tx1"},
					comparableRead{1, "key1.5", string(v1)}: []TxID{"tx2"},
					comparableRead{2, "key2.1", ""}:         []TxID{"tx1"},
					comparableRead{2, "key2.2", ""}:         []TxID{"tx3"},
					comparableRead{2, "key2.5", ""}:         []TxID{"tx3"},
				},
				nonBlindWritesPerTransaction: transactionToWrites{
					"tx1": tx1NonBlindWrites,
					"tx2": tx2NonBlindWrites,
					"tx3": tx3NonBlindWrites,
				},
				blindWritesPerTransaction: transactionToWrites{
					"tx3": tx3BlindWrites,
				},
			},
			expectedValidatedTx: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{},
				validTxBlindWrites:    transactionToWrites{},
				invalidTxIndices: map[TxID]bool{
					"tx1": true,
					"tx2": true,
					"tx3": true,
				},
			},
		},
		{
			name: "valid and invalid tx",
			preparedTx: &preparedTransactions{
				namespaceToReadEntries: namespaceToReads{
					1: &reads{
						keys:     []string{"key1.1", "key1.2", "key1.5"},
						versions: [][]byte{v1, v1, nil},
					},
					2: &reads{
						keys:     []string{"key2.1", "key2.2", "key2.5"},
						versions: [][]byte{nil, nil, nil},
					},
				},
				readToTransactionIndices: readToTransactions{
					comparableRead{1, "key1.1", string(v1)}: []TxID{"tx1"},
					comparableRead{1, "key1.2", string(v1)}: []TxID{"tx1"},
					comparableRead{1, "key1.5", ""}:         []TxID{"tx2"},
					comparableRead{2, "key2.1", ""}:         []TxID{"tx1"},
					comparableRead{2, "key2.2", ""}:         []TxID{"tx3"},
					comparableRead{2, "key2.5", ""}:         []TxID{"tx3"},
				},
				nonBlindWritesPerTransaction: transactionToWrites{
					"tx1": tx1NonBlindWrites,
					"tx2": tx2NonBlindWrites,
					"tx3": tx3NonBlindWrites,
				},
				blindWritesPerTransaction: transactionToWrites{
					"tx3": tx3BlindWrites,
				},
			},
			expectedValidatedTx: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx2": tx2NonBlindWrites,
				},
				validTxBlindWrites: transactionToWrites{},
				invalidTxIndices: map[TxID]bool{
					"tx1": true,
					"tx3": true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.preparedTxs <- tt.preparedTx
			validatedTxs := <-env.validatedTxs
			require.Equal(t, tt.expectedValidatedTx.validTxNonBlindWrites, validatedTxs.validTxNonBlindWrites)
			require.Equal(t, tt.expectedValidatedTx.validTxBlindWrites, validatedTxs.validTxBlindWrites)
			require.Equal(t, tt.expectedValidatedTx.invalidTxIndices, validatedTxs.invalidTxIndices)
		})
	}
}

// TODO: all the statement templates will be moved to a different package once we decide on the
// 		 chaincode deployment model.

var createTableStmtTmpt = `
CREATE TABLE IF NOT EXISTS %s (
    key varchar NOT NULL PRIMARY KEY,
    value bytea NOT NULL,
    version bytea
);
`

var createIndexStmtTmpt = `
CREATE INDEX idx_%s ON %s(version);
`

var validateFuncTmpt = `
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
        /* if the key does not exist in the committed state but read version is not null, we found a mismatch */
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

var commitFuncTmpt = `
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

var dropTableStmtTmpt = "DROP TABLE IF EXISTS %s"
var dropValidateFuncStmtTmpt = "DROP FUNCTION IF EXISTS validate_reads_%s"
var dropCommitFuncStmtTmpt = "DROP FUNCTION IF EXISTS commit_%s"

func populateDataWithCleanup(t *testing.T, conn *pgx.Conn, nsIDs []namespaceID, writes namespaceToWrites) {
	ctx := context.Background()

	for _, nsID := range nsIDs {
		tableName := tableNameForNamespace(nsID)

		// TODO: using []string{statement1, statement2, ...}, we can avoid certain repeated code
		createTableStmt := fmt.Sprintf(createTableStmtTmpt, tableName)
		_, err := conn.Exec(ctx, createTableStmt)
		require.NoError(t, err)
		t.Log("Created table", tableName)

		createIndexStmt := fmt.Sprintf(createIndexStmtTmpt, tableName, tableName)
		_, err = conn.Exec(ctx, createIndexStmt)
		require.NoError(t, err)
		t.Log("Created index", tableName)

		validateFunc := fmt.Sprintf(validateFuncTmpt, tableName, tableName, tableName, tableName, tableName, tableName)
		_, err = conn.Exec(ctx, validateFunc)
		require.NoError(t, err)
		t.Log("Created function", tableName)

		commitFunc := fmt.Sprintf(commitFuncTmpt, tableName, tableName)
		_, err = conn.Exec(ctx, commitFunc)
		require.NoError(t, err)
		t.Log("Created function", tableName)
	}

	for _, nsID := range nsIDs {
		writes := writes[nsID]
		tableName := tableNameForNamespace(nsID)

		// Though we can use multiple values within a single statement, for simplicity and also due to the usage
		// within the tests, we are making multiple calls to the database. When we are storing a large amount
		// of data, we should use multiple values within a single statement.
		for i, key := range writes.keys {
			_, err := conn.Exec(
				ctx,
				fmt.Sprintf("INSERT INTO %s (key, value, version) VALUES ($1, $2, $3)", tableName),
				key,
				writes.values[i],
				writes.versions[i],
			)
			require.NoError(t, err)
		}
	}

	t.Cleanup(func() {
		ctx := context.Background()

		dropStmtTmpts := []string{
			dropTableStmtTmpt,
			dropValidateFuncStmtTmpt,
			dropCommitFuncStmtTmpt,
		}

		for _, nsID := range nsIDs {
			tableName := tableNameForNamespace(nsID)

			for _, dropStmtTmpt := range dropStmtTmpts {
				_, err := conn.Exec(ctx, fmt.Sprintf(dropStmtTmpt, tableName))
				require.NoError(t, err)
			}
		}
	})
}
