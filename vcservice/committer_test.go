package vcservice

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
)

type committerTestEnv struct {
	c            *transactionCommitter
	validatedTxs chan *validatedTransactions
	txStatus     chan *protovcservice.TransactionStatus
	dbEnv        *databaseTestEnv
}

func newCommitterTestEnv(t *testing.T) *committerTestEnv {
	validatedTxs := make(chan *validatedTransactions, 10)
	txStatus := make(chan *protovcservice.TransactionStatus, 10)

	dbEnv := newDatabaseTestEnv(t)
	c := newCommitter(dbEnv.db, validatedTxs, txStatus)

	t.Cleanup(func() {
		close(validatedTxs)
		close(txStatus)
	})

	return &committerTestEnv{
		c:            c,
		validatedTxs: validatedTxs,
		txStatus:     txStatus,
		dbEnv:        dbEnv,
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

	k1_1 := []byte("key1.1")
	k1_2 := []byte("key1.2")
	k1_3 := []byte("key1.3")
	k1_4 := []byte("key1.4")
	k2_1 := []byte("key2.1")
	k2_2 := []byte("key2.2")
	k2_3 := []byte("key2.3")
	k2_4 := []byte("key2.4")
	k2_5 := []byte("key2.5")
	k2_6 := []byte("key2.6")

	committed := []byte{uint8(protovcservice.TransactionStatus_COMMITTED)}
	aborted := []byte{uint8(protovcservice.TransactionStatus_ABORTED_MVCC_CONFLICT)}

	env.dbEnv.populateDataWithCleanup(
		t,
		[]namespaceID{1, 2, txIDsStatusNameSpace},
		namespaceToWrites{
			1: {
				keys:     [][]byte{k1_1, k1_2, k1_3, k1_4},
				values:   [][]byte{[]byte("value1.1"), []byte("value1.2"), []byte("value1.3"), []byte("value1.4")},
				versions: [][]byte{v1, v1, v2, v2},
			},
			2: {
				keys:     [][]byte{k2_1, k2_2, k2_3, k2_4},
				values:   [][]byte{[]byte("value2.1"), []byte("value2.2"), []byte("value2.3"), []byte("value2.4")},
				versions: [][]byte{v0, v0, v1, v1},
			},
			txIDsStatusNameSpace: {
				keys:     [][]byte{[]byte("tx1"), []byte("tx2")},
				values:   [][]byte{committed, committed},
				versions: [][]byte{nil, nil},
			},
		},
	)

	// Note: the order of the sub-test is important
	tests := []struct {
		name               string
		txs                *validatedTransactions
		expectedTxStatuses *protovcservice.TransactionStatus
		expectedNsRows     namespaceToWrites
	}{
		{
			name: "commit_non_blid_writes",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx3": {
						1: {
							keys:     [][]byte{k1_1, k1_2},
							values:   [][]byte{[]byte("value1.1.1"), []byte("value1.2.1")},
							versions: [][]byte{v2, v2},
						},
						2: {
							keys:     [][]byte{k2_1, k2_2},
							values:   [][]byte{[]byte("value2.1.1"), []byte("value2.2.1")},
							versions: [][]byte{v1, v1},
						},
					},
				},
				validTxBlindWrites: transactionToWrites{},
				invalidTxIndices:   map[TxID]bool{},
			},
			expectedTxStatuses: &protovcservice.TransactionStatus{
				Status: map[string]protovcservice.TransactionStatus_Flag{
					"tx3": protovcservice.TransactionStatus_COMMITTED,
				},
			},
			expectedNsRows: namespaceToWrites{
				1: {
					keys:     [][]byte{k1_1, k1_2},
					values:   [][]byte{[]byte("value1.1.1"), []byte("value1.2.1")},
					versions: [][]byte{v2, v2},
				},
				2: {
					keys:     [][]byte{k2_1, k2_2},
					values:   [][]byte{[]byte("value2.1.1"), []byte("value2.2.1")},
					versions: [][]byte{v1, v1},
				},
				txIDsStatusNameSpace: {
					keys:     [][]byte{[]byte("tx3")},
					values:   [][]byte{committed},
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
							keys:     [][]byte{k1_1, k1_2},
							values:   [][]byte{[]byte("value1.1.2"), []byte("value1.2.2")},
							versions: [][]byte{nil, nil},
						},
						2: {
							keys:     [][]byte{k2_1, k2_2},
							values:   [][]byte{[]byte("value1.1.2"), []byte("value2.2.2")},
							versions: [][]byte{nil, nil},
						},
					},
				},
				invalidTxIndices: map[TxID]bool{},
			},
			expectedTxStatuses: &protovcservice.TransactionStatus{
				Status: map[string]protovcservice.TransactionStatus_Flag{
					"tx4": protovcservice.TransactionStatus_COMMITTED,
				},
			},
			expectedNsRows: namespaceToWrites{
				1: {
					keys:     [][]byte{k1_1, k1_2},
					values:   [][]byte{[]byte("value1.1.2"), []byte("value1.2.2")},
					versions: [][]byte{v3, v3},
				},
				2: {
					keys:     [][]byte{k2_1, k2_2},
					values:   [][]byte{[]byte("value1.1.2"), []byte("value2.2.2")},
					versions: [][]byte{v2, v2},
				},
				txIDsStatusNameSpace: {
					keys:     [][]byte{[]byte("tx4")},
					values:   [][]byte{committed},
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
							keys:     [][]byte{k1_1, k1_2},
							values:   [][]byte{[]byte("value1.1.3"), []byte("value1.2.3")},
							versions: [][]byte{v4, v4},
						},
					},
					"tx6": {
						1: {
							keys:     [][]byte{k1_3, k1_4},
							values:   [][]byte{[]byte("value1.3.1"), []byte("value1.4.1")},
							versions: [][]byte{v3, v3},
						},
					},
				},
				validTxBlindWrites: transactionToWrites{
					"tx7": {
						2: {
							keys:     [][]byte{k2_1, k2_2, k2_5},
							values:   [][]byte{[]byte("value2.1.3"), []byte("value2.2.3"), []byte("value2.5.1")},
							versions: [][]byte{nil, nil, nil},
						},
					},
					"tx8": {
						2: {
							keys:     [][]byte{k2_3, k2_4, k2_6},
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
			expectedTxStatuses: &protovcservice.TransactionStatus{
				Status: map[string]protovcservice.TransactionStatus_Flag{
					"tx5":  protovcservice.TransactionStatus_COMMITTED,
					"tx6":  protovcservice.TransactionStatus_COMMITTED,
					"tx7":  protovcservice.TransactionStatus_COMMITTED,
					"tx8":  protovcservice.TransactionStatus_COMMITTED,
					"tx9":  protovcservice.TransactionStatus_ABORTED_MVCC_CONFLICT,
					"tx10": protovcservice.TransactionStatus_ABORTED_MVCC_CONFLICT,
					"tx11": protovcservice.TransactionStatus_ABORTED_MVCC_CONFLICT,
				},
			},
			expectedNsRows: namespaceToWrites{
				1: {
					keys: [][]byte{k1_1, k1_2, k1_3, k1_4},
					values: [][]byte{
						[]byte("value1.1.3"), []byte("value1.2.3"), []byte("value1.3.1"), []byte("value1.4.1"),
					},
					versions: [][]byte{v4, v4, v3, v3},
				},
				2: {
					keys: [][]byte{k2_1, k2_2, k2_3, k2_4, k2_5, k2_6},
					values: [][]byte{
						[]byte("value2.1.3"), []byte("value2.2.3"), []byte("value2.3.1"),
						[]byte("value2.4.1"), []byte("value2.5.1"), []byte("value2.6.1"),
					},
					versions: [][]byte{v3, v3, v2, v2, v0, v0},
				},
				txIDsStatusNameSpace: {
					keys: [][]byte{
						[]byte("tx5"), []byte("tx6"), []byte("tx7"), []byte("tx8"),
						[]byte("tx9"), []byte("tx10"), []byte("tx11"),
					},
					values:   [][]byte{committed, committed, committed, committed, aborted, aborted, aborted},
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
			expectedTxStatuses: &protovcservice.TransactionStatus{
				Status: map[string]protovcservice.TransactionStatus_Flag{
					"tx12": protovcservice.TransactionStatus_ABORTED_MVCC_CONFLICT,
					"tx13": protovcservice.TransactionStatus_ABORTED_MVCC_CONFLICT,
					"tx14": protovcservice.TransactionStatus_ABORTED_MVCC_CONFLICT,
				},
			},
			expectedNsRows: namespaceToWrites{
				txIDsStatusNameSpace: {
					keys:     [][]byte{[]byte("tx12"), []byte("tx13"), []byte("tx14")},
					values:   [][]byte{aborted, aborted, aborted},
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
				env.dbEnv.rowExists(t, nsID, *expectedRows)
			}
		})
	}
}
