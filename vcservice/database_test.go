package vcservice

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
)

// TODO: all the statement templates will be moved to a different package once we decide on the
//	     chaincode deployment model.

const (
	queryKeyValueVersionSQLTmpt = "SELECT key, value, version FROM %s WHERE key = ANY($1)"
	queryTxStatusSQLTemplate    = "SELECT tx_id, status FROM tx_status WHERE tx_id = ANY($1)"

	ns1 = types.NamespaceID(1)
	ns2 = types.NamespaceID(2)
)

var (
	v0 = types.VersionNumber(0).Bytes()
	v1 = types.VersionNumber(1).Bytes()
)

type valueVersion struct {
	value   []byte
	version []byte
}

func TestValidateNamespaceReads(t *testing.T) {
	env := NewDatabaseTestEnv(t)

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
		[]int{int(ns1), int(ns2)},
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
		nil,
	)

	tests := []struct {
		name                    string
		nsID                    types.NamespaceID
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

			mismatchingReads, err := env.DB.validateNamespaceReads(tt.nsID, tt.r)
			require.NoError(t, err)
			requireReadsMatch(t, tt.expectedMismatchedReads, mismatchingReads)
		})
	}
}

// requireReadsMatch asserts that the specified readA is equal to specified
// readB ignoring the order of the elements. If there are duplicate elements,
// the number of appearances of each of them in both lists should match.
func requireReadsMatch(t *testing.T, readA, readB *reads, msgAndArgs ...any) {
	// the implementation is inspired the `require.ElementsMatch` function
	// https://github.com/stretchr/testify/blob/master/require/require.go#L95
	// https://github.com/stretchr/testify/blob/master/assert/assertions.go#L1052

	// is empty?
	if readA.empty() && readB.empty() {
		return
	}

	// create diff
	extraA, extraB := diffReads(readA, readB)
	if extraA.empty() && extraB.empty() {
		return
	}

	require.Fail(t,
		fmt.Sprintf("reads differ: extra elements in A=%v and extra elements in B=%v", extraA, extraB),
		msgAndArgs...)
}

// diffReads diffs two reads and returns reads that are only in A and only in B.
// If some key/version pair is present multiple times, each instance is counted separately (e.g. if something is 2x in A
// and 5x in B, it will be 0x in extraA and 3x in extraB). The order of items in both reads is ignored.
func diffReads(readA, readB *reads) (extraA, extraB *reads) {
	// the implementation is inspired on `diffLists` used in `require.ElementsMatch` function
	// https://github.com/stretchr/testify/blob/master/assert/assertions.go#L1086

	extraA = &reads{}
	extraB = &reads{}

	aLen := len(readA.keys)
	bLen := len(readB.keys)

	// Mark indexes in bValue that we already used
	visited := make([]bool, bLen)
	for i := 0; i < aLen; i++ {
		key, version := readA.keys[i], readA.versions[i]
		found := false
		for j := 0; j < bLen; j++ {
			if visited[j] {
				continue
			}

			if bytes.Equal(readB.keys[j], key) && bytes.Equal(readB.versions[j], version) {
				visited[j] = true
				found = true
				break
			}
		}
		if !found {
			extraA.append(key, version)
		}
	}

	for j := 0; j < bLen; j++ {
		if visited[j] {
			continue
		}
		extraB.append(readB.keys[j], readB.versions[j])
	}

	return extraA, extraB
}

func TestDBCommit(t *testing.T) {
	dbEnv := NewDatabaseTestEnv(t)

	dbEnv.populateDataWithCleanup(
		t,
		[]int{int(ns1), int(ns2)},
		nil,
		nil,
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
		types.MetaNamespaceID: {
			keys:     [][]byte{types.NamespaceID(3).Bytes(), types.NamespaceID(4).Bytes()},
			values:   [][]byte{[]byte("value7"), []byte("value8")},
			versions: [][]byte{v0, v0, v0},
		},
	}

	_, _, err := dbEnv.DB.commit(&statesToBeCommitted{newWrites: nsToWrites})
	require.NoError(t, err)

	_, _, err = dbEnv.DB.commit(&statesToBeCommitted{updateWrites: nsToWrites})
	require.NoError(t, err)
	dbEnv.rowExists(t, ns1, *nsToWrites[ns1])
	dbEnv.rowExists(t, ns2, *nsToWrites[ns2])
	dbEnv.rowExists(t, types.MetaNamespaceID, *nsToWrites[types.MetaNamespaceID])
	dbEnv.tableExists(t, types.NamespaceID(3))
	dbEnv.tableExists(t, types.NamespaceID(4))

	nsToWrites = namespaceToWrites{
		3: {
			keys:     [][]byte{k1, k2},
			values:   [][]byte{[]byte("value1"), []byte("value2")},
			versions: [][]byte{v0, v0},
		},
		4: {
			keys:     [][]byte{k4, k5},
			values:   [][]byte{[]byte("value4"), []byte("value5")},
			versions: [][]byte{v0, v0},
		},
	}
	_, _, err = dbEnv.DB.commit(&statesToBeCommitted{newWrites: nsToWrites})
	require.NoError(t, err)
	dbEnv.rowExists(t, 3, *nsToWrites[3])
	dbEnv.rowExists(t, 4, *nsToWrites[4])
}

func (env *DatabaseTestEnv) commitState(t *testing.T, nsToWrites namespaceToWrites) {
	for nsID, writes := range nsToWrites {
		_, err := env.DB.pool.Exec(context.Background(), fmt.Sprintf(`
			INSERT INTO %s (key, value, version)
			SELECT _key, _value, _version
			FROM UNNEST($1::bytea[], $2::bytea[], $3::bytea[]) AS t(_key, _value, _version);`,
			TableName(nsID)),
			writes.keys, writes.values, writes.versions,
		)
		require.NoError(t, err)
	}
}

func (env *DatabaseTestEnv) populateDataWithCleanup(
	t *testing.T, nsIDs []int, writes namespaceToWrites, batchStatus *protovcservice.TransactionStatus,
) {
	require.NoError(t, initDatabaseTables(context.Background(), env.DB.pool, nsIDs))

	_, _, err := env.DB.commit(&statesToBeCommitted{batchStatus: batchStatus})
	require.NoError(t, err)
	env.commitState(t, writes)

	t.Cleanup(func() {
		require.NoError(t, clearDatabaseTables(context.Background(), env.DB.pool, nsIDs))
	})
}

func (env *DatabaseTestEnv) fetchKeys(t *testing.T, nsID types.NamespaceID, keys [][]byte) map[string]*valueVersion {
	query := fmt.Sprintf(queryKeyValueVersionSQLTmpt, TableName(nsID))

	kvPairs, err := env.DB.pool.Query(context.Background(), query, keys)
	require.NoError(t, err)
	defer kvPairs.Close()

	actualRows := map[string]*valueVersion{}

	for kvPairs.Next() {
		var key []byte
		vv := &valueVersion{}

		require.NoError(t, kvPairs.Scan(&key, &vv.value, &vv.version))
		actualRows[string(key)] = vv
	}

	require.NoError(t, kvPairs.Err())

	return actualRows
}

func (env *DatabaseTestEnv) tableExists(t *testing.T, nsID types.NamespaceID) {
	query := fmt.Sprintf("SELECT table_name FROM information_schema.tables WHERE table_name = '%s'", TableName(nsID))
	names, err := env.DB.pool.Query(context.Background(), query)
	require.NoError(t, err)
	defer names.Close()
	require.True(t, names.Next())
}

func (env *DatabaseTestEnv) rowExists(t *testing.T, nsID types.NamespaceID, expectedRows namespaceWrites) {
	actualRows := env.fetchKeys(t, nsID, expectedRows.keys)

	assert.Len(t, actualRows, len(expectedRows.keys))
	for i, key := range expectedRows.keys {
		if assert.NotNil(t, actualRows[string(key)], "key: %s", string(key)) {
			assert.Equal(t, expectedRows.values[i], actualRows[string(key)].value, "key: %s", string(key))
			assert.Equal(t, expectedRows.versions[i], actualRows[string(key)].version, "key: %s", string(key))
		}
	}
}

func (env *DatabaseTestEnv) rowNotExists(t *testing.T, nsID types.NamespaceID, keys [][]byte) {
	actualRows := env.fetchKeys(t, nsID, keys)

	assert.Len(t, actualRows, 0)
	for key, valVer := range actualRows {
		assert.Fail(t, "key [%s] should not exist; value: [%s], version [%d]",
			key, string(valVer.value), types.VersionNumberFromBytes(valVer.version))
	}
}

func (env *DatabaseTestEnv) statusExists(t *testing.T, expected map[string]protoblocktx.Status) {
	expectedIds := make([][]byte, 0, len(expected))
	for id := range expected {
		expectedIds = append(expectedIds, []byte(id))
	}
	kvPairs, err := env.DB.pool.Query(context.Background(), queryTxStatusSQLTemplate, expectedIds)
	require.NoError(t, err)
	defer kvPairs.Close()

	actualRows := map[string]int{}

	for kvPairs.Next() {
		var key []byte
		var status int
		require.NoError(t, kvPairs.Scan(&key, &status))
		actualRows[string(key)] = status
	}

	require.NoError(t, kvPairs.Err())
	require.Equal(t, len(expectedIds), len(actualRows))
	for key, status := range expected {
		// "duplicated TX ID" status is never committed.
		if status == protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			continue
		}
		require.Equal(t, int(status), actualRows[key])
	}
}
