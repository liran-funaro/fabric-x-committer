package vcservice

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/pkg/types"
)

type prepareTestEnv struct {
	preparer    *transactionPreparer
	txBatch     chan *TransactionBatch
	preparedTxs chan *preparedTransactions
}

func newPrepareTestEnv(t *testing.T) *prepareTestEnv {
	txBatch := make(chan *TransactionBatch, 10)
	preparedTxs := make(chan *preparedTransactions, 10)
	preparer := newPreparer(txBatch, preparedTxs)

	t.Cleanup(func() {
		close(txBatch)
		close(preparedTxs)
	})

	return &prepareTestEnv{
		preparer:    preparer,
		txBatch:     txBatch,
		preparedTxs: preparedTxs,
	}
}

func TestPrepareTxWithReadsOnly(t *testing.T) {
	t.Parallel()

	env := newPrepareTestEnv(t)
	env.preparer.start(1)

	v0 := versionNumber(0).bytes()
	v1 := versionNumber(1).bytes()

	tx := &TransactionBatch{
		Transactions: []*TransactionWithID{
			{
				ID: "tx1",
				Namespaces: []*types.TxNamespace{
					{
						NsId: 1,
						ReadsOnly: []*types.Read{
							{Key: "key1", Version: v1},
							{Key: "key2", Version: v1},
							{Key: "key3", Version: nil},
						},
					},
					{
						NsId: 2,
						ReadsOnly: []*types.Read{
							{Key: "key4", Version: v0},
							{Key: "key5", Version: nil},
						},
					},
				},
			},
			{
				ID: "tx2",
				Namespaces: []*types.TxNamespace{
					{
						NsId: 1,
						ReadsOnly: []*types.Read{
							{Key: "key1", Version: v1},
							{Key: "key4", Version: v1},
							{Key: "key5", Version: nil},
						},
					},
					{
						NsId: 2,
						ReadsOnly: []*types.Read{
							{Key: "key4", Version: v1},
							{Key: "key5", Version: v0},
							{Key: "key6", Version: nil}},
					},
				},
			},
		},
	}

	expectedPreparedTxs := &preparedTransactions{
		namespaceToReadEntries: namespaceToReads{
			1: &reads{
				keys:     []string{"key1", "key2", "key3", "key4", "key5"},
				versions: [][]byte{v1, v1, nil, v1, nil},
			},
			2: &reads{
				keys:     []string{"key4", "key5", "key4", "key5", "key6"},
				versions: [][]byte{v0, nil, v1, v0, nil},
			},
		},
		readToTransactionIndices: readToTransactions{
			comparableRead{1, "key1", string(v1)}: []TxID{"tx1", "tx2"},
			comparableRead{1, "key2", string(v1)}: []TxID{"tx1"},
			comparableRead{1, "key3", ""}:         []TxID{"tx1"},
			comparableRead{1, "key4", string(v1)}: []TxID{"tx2"},
			comparableRead{1, "key5", ""}:         []TxID{"tx2"},
			comparableRead{2, "key4", string(v0)}: []TxID{"tx1"},
			comparableRead{2, "key5", ""}:         []TxID{"tx1"},
			comparableRead{2, "key4", string(v1)}: []TxID{"tx2"},
			comparableRead{2, "key5", string(v0)}: []TxID{"tx2"},
			comparableRead{2, "key6", ""}:         []TxID{"tx2"},
		},
		nonBlindWritesPerTransaction: transactionToWrites{},
		blindWritesPerTransaction:    transactionToWrites{},
	}

	env.txBatch <- tx
	preparedTxs := <-env.preparedTxs
	require.Equal(t, expectedPreparedTxs.namespaceToReadEntries, preparedTxs.namespaceToReadEntries)
	require.Equal(t, expectedPreparedTxs.readToTransactionIndices, preparedTxs.readToTransactionIndices)
	require.Equal(t, expectedPreparedTxs.nonBlindWritesPerTransaction, preparedTxs.nonBlindWritesPerTransaction)
	require.Equal(t, expectedPreparedTxs.blindWritesPerTransaction, preparedTxs.blindWritesPerTransaction)
}

func TestPrepareTxWithBlidWritesOnly(t *testing.T) {
	t.Parallel()

	env := newPrepareTestEnv(t)
	env.preparer.start(1)

	tx := &TransactionBatch{
		Transactions: []*TransactionWithID{
			{
				ID: "tx1",
				Namespaces: []*types.TxNamespace{
					{
						NsId: 1,
						BlindWrites: []*types.Write{
							{Key: "key1", Value: []byte("1")},
							{Key: "key2", Value: []byte("1")},
							{Key: "key3", Value: nil}},
					},
					{
						NsId:        2,
						BlindWrites: []*types.Write{{Key: "key1", Value: []byte("5")}},
					},
				},
			},
			{
				ID: "tx2",
				Namespaces: []*types.TxNamespace{
					{
						NsId: 1,
						BlindWrites: []*types.Write{
							{Key: "key4", Value: []byte("1")},
							{Key: "key5", Value: nil},
						},
					},
				},
			},
		},
	}

	expectedPreparedTxs := &preparedTransactions{
		namespaceToReadEntries:       namespaceToReads{},
		readToTransactionIndices:     readToTransactions{},
		nonBlindWritesPerTransaction: transactionToWrites{},
		blindWritesPerTransaction: transactionToWrites{
			"tx1": namespaceToWrites{
				1: &namespaceWrites{
					keys:    []string{"key1", "key2", "key3"},
					values:  [][]byte{[]byte("1"), []byte("1"), nil},
					version: [][]byte{nil, nil, nil},
				},
				2: &namespaceWrites{
					keys:    []string{"key1"},
					values:  [][]byte{[]byte("5")},
					version: [][]byte{nil},
				},
			},
			"tx2": namespaceToWrites{
				1: &namespaceWrites{
					keys:    []string{"key4", "key5"},
					values:  [][]byte{[]byte("1"), nil},
					version: [][]byte{nil, nil},
				},
			},
		},
	}

	env.txBatch <- tx
	preparedTxs := <-env.preparedTxs
	require.Equal(t, expectedPreparedTxs.namespaceToReadEntries, preparedTxs.namespaceToReadEntries)
	require.Equal(t, expectedPreparedTxs.readToTransactionIndices, preparedTxs.readToTransactionIndices)
	require.Equal(t, expectedPreparedTxs.nonBlindWritesPerTransaction, preparedTxs.nonBlindWritesPerTransaction)
	require.Equal(t, expectedPreparedTxs.blindWritesPerTransaction, preparedTxs.blindWritesPerTransaction)
}

func TestPrepareTxWithReadWritesOnly(t *testing.T) {
	t.Parallel()

	env := newPrepareTestEnv(t)
	env.preparer.start(1)

	v0 := versionNumber(0).bytes()
	v1 := versionNumber(1).bytes()
	v2 := versionNumber(2).bytes()

	tx := &TransactionBatch{
		Transactions: []*TransactionWithID{
			{
				ID: "tx1",
				Namespaces: []*types.TxNamespace{
					{
						NsId: 1,
						ReadWrites: []*types.ReadWrite{
							{Key: "key1", Version: v1, Value: []byte("v1")},
							{Key: "key2", Version: v1, Value: []byte("v2")},
							{Key: "key3", Version: nil, Value: []byte("v3")},
						},
					},
					{
						NsId: 2,
						ReadWrites: []*types.ReadWrite{
							{Key: "key4", Version: v0, Value: []byte("v4")},
							{Key: "key5", Version: nil, Value: []byte("v5")},
						},
					},
				},
			},
			{
				ID: "tx2",
				Namespaces: []*types.TxNamespace{
					{
						NsId: 1,
						ReadWrites: []*types.ReadWrite{
							{Key: "key4", Version: v1, Value: []byte("v4")},
							{Key: "key5", Version: nil, Value: []byte("v5")},
						},
					},
					{
						NsId: 2,
						ReadWrites: []*types.ReadWrite{
							{Key: "key6", Version: nil, Value: []byte("v6")},
						},
					},
				},
			},
		},
	}

	expectedPreparedTxs := &preparedTransactions{
		namespaceToReadEntries: namespaceToReads{
			1: &reads{
				keys:     []string{"key1", "key2", "key3", "key4", "key5"},
				versions: [][]byte{v1, v1, nil, v1, nil}},
			2: &reads{
				keys:     []string{"key4", "key5", "key6"},
				versions: [][]byte{v0, nil, nil},
			},
		},
		readToTransactionIndices: readToTransactions{
			comparableRead{1, "key1", string(v1)}: []TxID{"tx1"},
			comparableRead{1, "key2", string(v1)}: []TxID{"tx1"},
			comparableRead{1, "key3", ""}:         []TxID{"tx1"},
			comparableRead{1, "key4", string(v1)}: []TxID{"tx2"},
			comparableRead{1, "key5", ""}:         []TxID{"tx2"},
			comparableRead{2, "key4", string(v0)}: []TxID{"tx1"},
			comparableRead{2, "key5", ""}:         []TxID{"tx1"},
			comparableRead{2, "key6", ""}:         []TxID{"tx2"},
		},
		nonBlindWritesPerTransaction: transactionToWrites{
			"tx1": namespaceToWrites{
				1: &namespaceWrites{
					keys:    []string{"key1", "key2", "key3"},
					values:  [][]byte{[]byte("v1"), []byte("v2"), []byte("v3")},
					version: [][]byte{v2, v2, v0},
				},
				2: &namespaceWrites{
					keys:    []string{"key4", "key5"},
					values:  [][]byte{[]byte("v4"), []byte("v5")},
					version: [][]byte{v1, v0},
				},
			},
			"tx2": namespaceToWrites{
				1: &namespaceWrites{
					keys:    []string{"key4", "key5"},
					values:  [][]byte{[]byte("v4"), []byte("v5")},
					version: [][]byte{v2, v0},
				},
				2: &namespaceWrites{
					keys:    []string{"key6"},
					values:  [][]byte{[]byte("v6")},
					version: [][]byte{v0},
				},
			},
		},
		blindWritesPerTransaction: transactionToWrites{},
	}

	env.txBatch <- tx
	preparedTxs := <-env.preparedTxs
	require.Equal(t, expectedPreparedTxs.namespaceToReadEntries, preparedTxs.namespaceToReadEntries)
	require.Equal(t, expectedPreparedTxs.readToTransactionIndices, preparedTxs.readToTransactionIndices)
	require.Equal(t, expectedPreparedTxs.nonBlindWritesPerTransaction, preparedTxs.nonBlindWritesPerTransaction)
	require.Equal(t, expectedPreparedTxs.blindWritesPerTransaction, preparedTxs.blindWritesPerTransaction)
}

func TestPrepareTx(t *testing.T) {
	t.Parallel()

	env := newPrepareTestEnv(t)
	env.preparer.start(1)

	v0 := versionNumber(0).bytes()
	v1 := versionNumber(1).bytes()
	v2 := versionNumber(2).bytes()
	v3 := versionNumber(3).bytes()
	v4 := versionNumber(4).bytes()
	v8 := versionNumber(8).bytes()
	v9 := versionNumber(9).bytes()
	v10 := versionNumber(10).bytes()

	tx := &TransactionBatch{
		Transactions: []*TransactionWithID{
			{
				ID: "tx1",
				Namespaces: []*types.TxNamespace{
					{
						NsId: 1,
						ReadsOnly: []*types.Read{
							{Key: "key1", Version: v1},
							{Key: "key2", Version: v2},
						},
						ReadWrites: []*types.ReadWrite{
							{Key: "key3", Version: v3, Value: []byte("v3")},
						},
						BlindWrites: []*types.Write{
							{Key: "key4", Value: []byte("v4")},
						},
					},
					{
						NsId: 2,
						ReadsOnly: []*types.Read{
							{Key: "key5", Version: nil},
						},
						ReadWrites: []*types.ReadWrite{
							{Key: "key6", Version: nil, Value: []byte("v6")},
						},
						BlindWrites: []*types.Write{
							{Key: "key7", Value: []byte("v7")},
						},
					},
				},
			},
			{
				ID: "tx2",
				Namespaces: []*types.TxNamespace{
					{
						NsId: 1,
						ReadsOnly: []*types.Read{
							{Key: "key8", Version: v8},
						},
						ReadWrites: []*types.ReadWrite{
							{Key: "key9", Version: v9, Value: []byte("v9")},
						},
						BlindWrites: []*types.Write{
							{Key: "key10", Value: []byte("v10")},
						},
					},
				},
			},
		},
	}

	expectedPreparedTxs := &preparedTransactions{
		namespaceToReadEntries: namespaceToReads{
			1: &reads{
				keys:     []string{"key1", "key2", "key3", "key8", "key9"},
				versions: [][]byte{v1, v2, v3, v8, v9},
			},
			2: &reads{
				keys:     []string{"key5", "key6"},
				versions: [][]byte{nil, nil},
			},
		},
		readToTransactionIndices: readToTransactions{
			comparableRead{1, "key1", string(v1)}: []TxID{"tx1"},
			comparableRead{1, "key2", string(v2)}: []TxID{"tx1"},
			comparableRead{1, "key3", string(v3)}: []TxID{"tx1"},
			comparableRead{1, "key8", string(v8)}: []TxID{"tx2"},
			comparableRead{1, "key9", string(v9)}: []TxID{"tx2"},
			comparableRead{2, "key5", ""}:         []TxID{"tx1"},
			comparableRead{2, "key6", ""}:         []TxID{"tx1"},
		},
		nonBlindWritesPerTransaction: transactionToWrites{
			"tx1": namespaceToWrites{
				1: &namespaceWrites{
					keys:    []string{"key3"},
					values:  [][]byte{[]byte("v3")},
					version: [][]byte{v4},
				},
				2: &namespaceWrites{
					keys:    []string{"key6"},
					values:  [][]byte{[]byte("v6")},
					version: [][]byte{v0},
				},
			},
			"tx2": namespaceToWrites{
				1: &namespaceWrites{
					keys:    []string{"key9"},
					values:  [][]byte{[]byte("v9")},
					version: [][]byte{v10},
				},
			},
		},
		blindWritesPerTransaction: transactionToWrites{
			"tx1": namespaceToWrites{
				1: &namespaceWrites{
					keys:    []string{"key4"},
					values:  [][]byte{[]byte("v4")},
					version: [][]byte{nil},
				},
				2: &namespaceWrites{
					keys:    []string{"key7"},
					values:  [][]byte{[]byte("v7")},
					version: [][]byte{nil},
				},
			},
			"tx2": namespaceToWrites{
				1: &namespaceWrites{
					keys:    []string{"key10"},
					values:  [][]byte{[]byte("v10")},
					version: [][]byte{nil},
				},
			},
		},
	}

	env.txBatch <- tx
	preparedTxs := <-env.preparedTxs
	require.Equal(t, expectedPreparedTxs.namespaceToReadEntries, preparedTxs.namespaceToReadEntries)
	require.Equal(t, expectedPreparedTxs.readToTransactionIndices, preparedTxs.readToTransactionIndices)
	require.Equal(t, expectedPreparedTxs.nonBlindWritesPerTransaction, preparedTxs.nonBlindWritesPerTransaction)
	require.Equal(t, expectedPreparedTxs.blindWritesPerTransaction, preparedTxs.blindWritesPerTransaction)
}
