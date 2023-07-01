package vcservice

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v4"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
)

type committerTestEnv struct {
	c            *transactionCommitter
	validatedTxs chan *validatedTransactions
	txStatus     chan transactionCommitStatus
}

func newCommitterTestEnv(t *testing.T) *committerTestEnv {
	db := runner.YugabyteDB{}
	require.NoError(t, db.Start())

	validatedTxs := make(chan *validatedTransactions, 10)
	txStatus := make(chan transactionCommitStatus, 10)

	psqlInfo := db.ConnectionSettings().DataSourceName()
	conn, err := pgx.Connect(context.Background(), psqlInfo)
	require.NoError(t, err)

	c := newCommitter(conn, validatedTxs, txStatus)

	t.Cleanup(func() {
		close(validatedTxs)
		close(txStatus)
		_ = db.Stop()
		_ = conn.Close(context.Background())
	})

	return &committerTestEnv{
		c:            c,
		validatedTxs: validatedTxs,
		txStatus:     txStatus,
	}
}

func TestCommit(t *testing.T) {
	env := newCommitterTestEnv(t)
	env.c.start(1)

	v0 := versionNumber(0).bytes()
	v1 := versionNumber(1).bytes()
	v2 := versionNumber(2).bytes()
	v3 := versionNumber(3).bytes()
	v4 := versionNumber(4).bytes()

	populateDataWithCleanup(
		t,
		env.c.databaseConnection,
		[]namespaceID{1, 2, txIDsStatusNameSpace},
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
			txIDsStatusNameSpace: {
				keys:     []string{"tx1", "tx2"},
				values:   [][]byte{{uint8(COMMITTED)}, {uint8(COMMITTED)}},
				versions: [][]byte{nil, nil},
			},
		},
	)

	// Note: the order of the sub-test is important
	tests := []struct {
		name               string
		txs                *validatedTransactions
		expectedTxStatuses transactionCommitStatus
		expectedNsRows     namespaceToWrites
	}{
		{
			name: "commit_non_blid_writes",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx3": {
						1: {
							keys:     []string{"key1.1", "key1.2"},
							values:   [][]byte{[]byte("value1.1.1"), []byte("value1.2.1")},
							versions: [][]byte{v2, v2},
						},
						2: {
							keys:     []string{"key2.1", "key2.2"},
							values:   [][]byte{[]byte("value2.1.1"), []byte("value2.2.1")},
							versions: [][]byte{v1, v1},
						},
					},
				},
				validTxBlindWrites: transactionToWrites{},
				invalidTxIndices:   map[TxID]bool{},
			},
			expectedTxStatuses: transactionCommitStatus{
				"tx3": COMMITTED,
			},
			expectedNsRows: namespaceToWrites{
				1: {
					keys:     []string{"key1.1", "key1.2"},
					values:   [][]byte{[]byte("value1.1.1"), []byte("value1.2.1")},
					versions: [][]byte{v2, v2},
				},
				2: {
					keys:     []string{"key2.1", "key2.2"},
					values:   [][]byte{[]byte("value2.1.1"), []byte("value2.2.1")},
					versions: [][]byte{v1, v1},
				},
				txIDsStatusNameSpace: {
					keys:     []string{"tx3"},
					values:   [][]byte{{uint8(COMMITTED)}},
					versions: [][]byte{nil},
				},
			},
		},
		{
			name: "commit_blind_writes",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{},
				validTxBlindWrites: transactionToWrites{
					"tx4": {
						1: {
							keys:     []string{"key1.1", "key1.2"},
							values:   [][]byte{[]byte("value1.1.2"), []byte("value1.2.2")},
							versions: [][]byte{nil, nil},
						},
						2: {
							keys:     []string{"key2.1", "key2.2"},
							values:   [][]byte{[]byte("value1.1.2"), []byte("value2.2.2")},
							versions: [][]byte{nil, nil},
						},
					},
				},
				invalidTxIndices: map[TxID]bool{},
			},
			expectedTxStatuses: transactionCommitStatus{
				"tx4": COMMITTED,
			},
			expectedNsRows: namespaceToWrites{
				1: {
					keys:     []string{"key1.1", "key1.2"},
					values:   [][]byte{[]byte("value1.1.2"), []byte("value1.2.2")},
					versions: [][]byte{v3, v3},
				},
				2: {
					keys:     []string{"key2.1", "key2.2"},
					values:   [][]byte{[]byte("value1.1.2"), []byte("value2.2.2")},
					versions: [][]byte{v2, v2},
				},
				txIDsStatusNameSpace: {
					keys:     []string{"tx4"},
					values:   [][]byte{{uint8(COMMITTED)}},
					versions: [][]byte{nil},
				},
			},
		},
		{
			name: "commit_blind_and_nonblind_writes",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx5": {
						1: {
							keys:     []string{"key1.1", "key1.2"},
							values:   [][]byte{[]byte("value1.1.3"), []byte("value1.2.3")},
							versions: [][]byte{v4, v4},
						},
					},
					"tx6": {
						1: {
							keys:     []string{"key1.3", "key1.4"},
							values:   [][]byte{[]byte("value1.3.1"), []byte("value1.4.1")},
							versions: [][]byte{v3, v3},
						},
					},
				},
				validTxBlindWrites: transactionToWrites{
					"tx7": {
						2: {
							keys:     []string{"key2.1", "key2.2", "key2.5"},
							values:   [][]byte{[]byte("value2.1.3"), []byte("value2.2.3"), []byte("value2.5.1")},
							versions: [][]byte{nil, nil, nil},
						},
					},
					"tx8": {
						2: {
							keys:     []string{"key2.3", "key2.4", "key2.6"},
							values:   [][]byte{[]byte("value2.3.1"), []byte("value2.4.1"), []byte("value2.6.1")},
							versions: [][]byte{nil, nil, nil},
						},
					},
				},
				invalidTxIndices: map[TxID]bool{
					"tx9":  true,
					"tx10": true,
					"tx11": true,
				},
			},
			expectedTxStatuses: transactionCommitStatus{
				"tx5":  COMMITTED,
				"tx6":  COMMITTED,
				"tx7":  COMMITTED,
				"tx8":  COMMITTED,
				"tx9":  ABORTED_MVCC_CONFLICT,
				"tx10": ABORTED_MVCC_CONFLICT,
				"tx11": ABORTED_MVCC_CONFLICT,
			},
			expectedNsRows: namespaceToWrites{
				1: {
					keys:     []string{"key1.1", "key1.2", "key1.3", "key1.4"},
					values:   [][]byte{[]byte("value1.1.3"), []byte("value1.2.3"), []byte("value1.3.1"), []byte("value1.4.1")},
					versions: [][]byte{v4, v4, v3, v3},
				},
				2: {
					keys: []string{"key2.1", "key2.2", "key2.3", "key2.4", "key2.5", "key2.6"},
					values: [][]byte{[]byte("value2.1.3"), []byte("value2.2.3"), []byte("value2.3.1"),
						[]byte("value2.4.1"), []byte("value2.5.1"), []byte("value2.6.1")},
					versions: [][]byte{v3, v3, v2, v2, v0, v0},
				},
				txIDsStatusNameSpace: {
					keys: []string{"tx5", "tx6", "tx7", "tx8", "tx9", "tx10", "tx11"},
					values: [][]byte{
						{uint8(COMMITTED)}, {uint8(COMMITTED)}, {uint8(COMMITTED)}, {uint8(COMMITTED)},
						{uint8(ABORTED_MVCC_CONFLICT)}, {uint8(ABORTED_MVCC_CONFLICT)}, {uint8(ABORTED_MVCC_CONFLICT)}},
					versions: [][]byte{nil, nil, nil, nil, nil, nil, nil},
				},
			},
		},
		{
			name: "all invalid txs",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{},
				validTxBlindWrites:    transactionToWrites{},
				invalidTxIndices: map[TxID]bool{
					"tx12": true,
					"tx13": true,
					"tx14": true,
				},
			},
			expectedTxStatuses: transactionCommitStatus{
				"tx12": ABORTED_MVCC_CONFLICT,
				"tx13": ABORTED_MVCC_CONFLICT,
				"tx14": ABORTED_MVCC_CONFLICT,
			},
			expectedNsRows: namespaceToWrites{
				txIDsStatusNameSpace: {
					keys:     []string{"tx12", "tx13", "tx14"},
					values:   [][]byte{{uint8(ABORTED_MVCC_CONFLICT)}, {uint8(ABORTED_MVCC_CONFLICT)}, {uint8(ABORTED_MVCC_CONFLICT)}},
					versions: [][]byte{nil, nil, nil},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.validatedTxs <- tt.txs
			txStatus := <-env.txStatus
			require.Equal(t, tt.expectedTxStatuses, txStatus)
			for nsID, expectedRows := range tt.expectedNsRows {
				env.rowExists(t, nsID, *expectedRows)
			}
		})
	}
}

func (c *committerTestEnv) rowExists(t *testing.T, nsID namespaceID, expectedRows namespaceWrites) {
	query := fmt.Sprintf("SELECT key, value, version FROM %s WHERE key = ANY($1)", tableNameForNamespace(nsID))

	kvPairs, err := c.c.databaseConnection.Query(context.Background(), query, expectedRows.keys)
	require.NoError(t, err)
	defer kvPairs.Close()

	type valver struct {
		value   []byte
		version []byte
	}

	actualRows := map[string]*valver{}

	for kvPairs.Next() {
		var key string
		vv := &valver{}

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
