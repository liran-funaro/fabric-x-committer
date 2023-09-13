package vcservice

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
)

// TODO: all the statement templates will be moved to a different package once we decide on the
//	     chaincode deployment model.

const (
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

	cs := dbRunner.ConnectionSettings()
	port, err := strconv.Atoi(cs.Port)
	require.NoError(t, err)

	config := &DatabaseConfig{
		Host:           cs.Host,
		Port:           port,
		Username:       cs.User,
		Password:       cs.Password,
		MaxConnections: 20,
		MinConnections: 10,
	}
	db, err := newDatabase(config)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.close()
		assert.NoError(t, dbRunner.Stop())
	})

	return &databaseTestEnv{
		db:       db,
		dbRunner: dbRunner,
	}
}

func TestValidateNamespaceReads(t *testing.T) {
	env := newDatabaseTestEnv(t)

	k1 := []byte("key1")
	k2 := []byte("key2")
	k3 := []byte("key3")
	k4 := []byte("key4")
	k5 := []byte("key5")
	k6 := []byte("key6")
	k7 := []byte("key7")
	k8 := []byte("key8")
	k9 := []byte("key9")

	env.populateDataWithCleanup(
		t,
		[]namespaceID{ns1, ns2},
		namespaceToWrites{
			ns1: {
				keys:     [][]byte{k1, k2, k3},
				values:   [][]byte{[]byte("value1"), []byte("value2"), []byte("value3")},
				versions: [][]byte{v0, v0, v0},
			},
			ns2: {
				keys:     [][]byte{k4, k5, k6},
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
				keys:     [][]byte{k4, k5, k6},
				versions: [][]byte{nil, nil, nil},
			},
			expectedMismatchedReads: &reads{},
		},
		{
			name: "reads of only non-existing keys and some mismatching versions",
			nsID: ns2,
			r: &reads{
				keys:     [][]byte{k7, k8, k9},
				versions: [][]byte{nil, v0, nil},
			},
			expectedMismatchedReads: &reads{
				keys:     [][]byte{k8},
				versions: [][]byte{v0},
			},
		},
		{
			name: "reads of only non-existing keys and all mismatching versions",
			nsID: ns2,
			r: &reads{
				keys:     [][]byte{k7, k8, k9},
				versions: [][]byte{v1, v0, v1},
			},
			expectedMismatchedReads: &reads{
				keys:     [][]byte{k7, k8, k9},
				versions: [][]byte{v1, v0, v1},
			},
		},
		{
			name: "reads of existing keys and all matching versions",
			nsID: ns1,
			r: &reads{
				keys:     [][]byte{k1, k2, k3},
				versions: [][]byte{v0, v0, v0},
			},
			expectedMismatchedReads: &reads{},
		},
		{
			name: "reads of existing keys and some mismatching versions",
			nsID: ns1,
			r: &reads{
				keys:     [][]byte{k1, k2, k3},
				versions: [][]byte{v1, v0, v1},
			},
			expectedMismatchedReads: &reads{
				keys:     [][]byte{k1, k3},
				versions: [][]byte{v1, v1},
			},
		},
		{
			name: "reads of existing keys and all mismatching versions",
			nsID: ns2,
			r: &reads{
				keys:     [][]byte{k4, k5, k6},
				versions: [][]byte{v0, v0, v0},
			},
			expectedMismatchedReads: &reads{
				keys:     [][]byte{k4, k5, k6},
				versions: [][]byte{v0, v0, v0},
			},
		},
		{
			name: "reads of existing and non-existing keys and some mismatching versions",
			nsID: ns2,
			r: &reads{
				keys:     [][]byte{k4, k5, k6, k7, k8, k9},
				versions: [][]byte{v1, v0, v1, nil, v0, nil},
			},
			expectedMismatchedReads: &reads{
				keys:     [][]byte{k5, k8},
				versions: [][]byte{v0, v0},
			},
		},
	}

	for _, test := range tests {
		tt := test
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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

	k1 := []byte("key1")
	k2 := []byte("key2")
	k3 := []byte("key3")
	k4 := []byte("key4")
	k5 := []byte("key5")
	k6 := []byte("key6")

	nsToWrites := namespaceToWrites{
		ns1: {
			keys:     [][]byte{k1, k2, k3},
			values:   [][]byte{[]byte("value1"), []byte("value2"), []byte("value3")},
			versions: [][]byte{v0, v0, v0},
		},
		ns2: {
			keys:     [][]byte{k4, k5, k6},
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
			_, err := env.db.pool.Exec(ctx, stmt)
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
				_, err := env.db.pool.Exec(ctx, fmt.Sprintf(dropStmtTmpt, tableName))
				require.NoError(t, err)
			}
		}
	})
}

func (env *databaseTestEnv) rowExists(t *testing.T, nsID namespaceID, expectedRows namespaceWrites) {
	query := fmt.Sprintf(queryKeyValueVersionSQLTmpt, tableNameForNamespace(nsID))

	kvPairs, err := env.db.pool.Query(context.Background(), query, expectedRows.keys)
	require.NoError(t, err)
	defer kvPairs.Close()

	type valueVersion struct {
		value   []byte
		version []byte
	}

	actualRows := map[string]*valueVersion{}

	for kvPairs.Next() {
		var key []byte
		vv := &valueVersion{}

		require.NoError(t, kvPairs.Scan(&key, &vv.value, &vv.version))
		actualRows[string(key)] = vv
	}

	require.NoError(t, kvPairs.Err())
	require.Equal(t, len(expectedRows.keys), len(actualRows))
	for i, key := range expectedRows.keys {
		require.Equal(t, expectedRows.values[i], actualRows[string(key)].value)
		require.Equal(t, expectedRows.versions[i], actualRows[string(key)].version)
	}
}
