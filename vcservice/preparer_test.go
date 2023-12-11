package vcservice

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
)

type prepareTestEnv struct {
	preparer    *transactionPreparer
	txBatch     chan *protovcservice.TransactionBatch
	preparedTxs chan *preparedTransactions
}

func newPrepareTestEnv(t *testing.T) *prepareTestEnv {
	txBatch := make(chan *protovcservice.TransactionBatch, 10)
	preparedTxs := make(chan *preparedTransactions, 10)
	metrics := newVCServiceMetrics()
	ctx, cancel := context.WithCancel(context.Background())
	preparer := newPreparer(ctx, txBatch, preparedTxs, metrics)

	t.Cleanup(func() {
		cancel()
		close(txBatch)
		preparer.wg.Wait()
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

	k1 := []byte("key1")
	k2 := []byte("key2")
	k3 := []byte("key3")
	k4 := []byte("key4")
	k5 := []byte("key5")
	k6 := []byte("key6")

	tx := &protovcservice.TransactionBatch{
		Transactions: []*protovcservice.Transaction{
			{
				ID: "tx1",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
						ReadsOnly: []*protoblocktx.Read{
							{Key: k1, Version: v1},
							{Key: k2, Version: v1},
							{Key: k3, Version: nil},
						},
					},
					{
						NsId: 2,
						ReadsOnly: []*protoblocktx.Read{
							{Key: k4, Version: v0},
							{Key: k5, Version: nil},
						},
					},
				},
			},
			{
				ID: "tx2",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
						ReadsOnly: []*protoblocktx.Read{
							{Key: k1, Version: v1},
							{Key: k4, Version: v1},
							{Key: k5, Version: nil},
						},
					},
					{
						NsId: 2,
						ReadsOnly: []*protoblocktx.Read{
							{Key: k4, Version: v1},
							{Key: k5, Version: v0},
							{Key: k6, Version: nil},
						},
					},
				},
			},
		},
	}

	expectedPreparedTxs := &preparedTransactions{
		namespaceToReadEntries: namespaceToReads{
			1: &reads{
				keys:     [][]byte{k1, k2, k3, k4, k5},
				versions: [][]byte{v1, v1, nil, v1, nil},
			},
			2: &reads{
				keys:     [][]byte{k4, k5, k4, k5, k6},
				versions: [][]byte{v0, nil, v1, v0, nil},
			},
		},
		readToTransactionIndices: readToTransactions{
			comparableRead{1, string(k1), string(v1)}: []txID{"tx1", "tx2"},
			comparableRead{1, string(k2), string(v1)}: []txID{"tx1"},
			comparableRead{1, string(k3), ""}:         []txID{"tx1"},
			comparableRead{1, string(k4), string(v1)}: []txID{"tx2"},
			comparableRead{1, string(k5), ""}:         []txID{"tx2"},
			comparableRead{2, string(k4), string(v0)}: []txID{"tx1"},
			comparableRead{2, string(k5), ""}:         []txID{"tx1"},
			comparableRead{2, string(k4), string(v1)}: []txID{"tx2"},
			comparableRead{2, string(k5), string(v0)}: []txID{"tx2"},
			comparableRead{2, string(k6), ""}:         []txID{"tx2"},
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

	k1 := []byte("key1")
	k2 := []byte("key2")
	k3 := []byte("key3")
	k4 := []byte("key4")
	k5 := []byte("key5")

	tx := &protovcservice.TransactionBatch{
		Transactions: []*protovcservice.Transaction{
			{
				ID: "tx1",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
						BlindWrites: []*protoblocktx.Write{
							{Key: k1, Value: []byte("1")},
							{Key: k2, Value: []byte("1")},
							{Key: k3, Value: nil},
						},
					},
					{
						NsId: 2,
						BlindWrites: []*protoblocktx.Write{{
							Key:   k1,
							Value: []byte("5"),
						}},
					},
				},
			},
			{
				ID: "tx2",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
						BlindWrites: []*protoblocktx.Write{
							{Key: k4, Value: []byte("1")},
							{Key: k5, Value: nil},
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
					keys:     [][]byte{k1, k2, k3},
					values:   [][]byte{[]byte("1"), []byte("1"), nil},
					versions: [][]byte{nil, nil, nil},
				},
				2: &namespaceWrites{
					keys:     [][]byte{k1},
					values:   [][]byte{[]byte("5")},
					versions: [][]byte{nil},
				},
			},
			"tx2": namespaceToWrites{
				1: &namespaceWrites{
					keys:     [][]byte{k4, k5},
					values:   [][]byte{[]byte("1"), nil},
					versions: [][]byte{nil, nil},
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

	k1 := []byte("key1")
	k2 := []byte("key2")
	k3 := []byte("key3")
	k4 := []byte("key4")
	k5 := []byte("key5")
	k6 := []byte("key6")
	k7 := []byte("key7")

	tx := &protovcservice.TransactionBatch{
		Transactions: []*protovcservice.Transaction{
			{
				ID: "tx1",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
						ReadWrites: []*protoblocktx.ReadWrite{
							{Key: k1, Version: v1, Value: []byte("v1")},
							{Key: k2, Version: v1, Value: []byte("v2")},
							{Key: k3, Version: nil, Value: []byte("v3")},
						},
					},
					{
						NsId: 2,
						ReadWrites: []*protoblocktx.ReadWrite{
							{Key: k4, Version: v0, Value: []byte("v4")},
							{Key: k5, Version: nil, Value: []byte("v5")},
						},
					},
				},
			},
			{
				ID: "tx2",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
						ReadWrites: []*protoblocktx.ReadWrite{
							{Key: k4, Version: v1, Value: []byte("v4")},
							{Key: k5, Version: nil, Value: []byte("v5")},
						},
					},
					{
						NsId: 2,
						ReadWrites: []*protoblocktx.ReadWrite{
							{Key: k6, Version: nil, Value: []byte("v6")},
							{Key: k7, Version: nil, Value: nil},
						},
					},
				},
			},
		},
	}

	expectedPreparedTxs := &preparedTransactions{
		namespaceToReadEntries: namespaceToReads{
			1: &reads{
				keys:     [][]byte{k1, k2, k4},
				versions: [][]byte{v1, v1, v1},
			},
			2: &reads{
				keys:     [][]byte{k4},
				versions: [][]byte{v0},
			},
		},
		readToTransactionIndices: readToTransactions{
			comparableRead{1, string(k1), string(v1)}: []txID{"tx1"},
			comparableRead{1, string(k2), string(v1)}: []txID{"tx1"},
			comparableRead{1, string(k3), ""}:         []txID{"tx1"},
			comparableRead{1, string(k4), string(v1)}: []txID{"tx2"},
			comparableRead{1, string(k5), ""}:         []txID{"tx2"},
			comparableRead{2, string(k4), string(v0)}: []txID{"tx1"},
			comparableRead{2, string(k5), ""}:         []txID{"tx1"},
			comparableRead{2, string(k6), ""}:         []txID{"tx2"},
			comparableRead{2, string(k7), ""}:         []txID{"tx2"},
		},
		nonBlindWritesPerTransaction: transactionToWrites{
			"tx1": namespaceToWrites{
				1: &namespaceWrites{
					keys:     [][]byte{k1, k2},
					values:   [][]byte{[]byte("v1"), []byte("v2")},
					versions: [][]byte{v2, v2},
				},
				2: &namespaceWrites{
					keys:     [][]byte{k4},
					values:   [][]byte{[]byte("v4")},
					versions: [][]byte{v1},
				},
			},
			"tx2": namespaceToWrites{
				1: &namespaceWrites{
					keys:     [][]byte{k4},
					values:   [][]byte{[]byte("v4")},
					versions: [][]byte{v2},
				},
			},
		},
		blindWritesPerTransaction: transactionToWrites{},
		newWrites: transactionToWrites{
			"tx1": namespaceToWrites{
				1: &namespaceWrites{
					keys:     [][]byte{k3},
					values:   [][]byte{[]byte("v3")},
					versions: [][]byte{nil},
				},
				2: &namespaceWrites{
					keys:     [][]byte{k5},
					values:   [][]byte{[]byte("v5")},
					versions: [][]byte{nil},
				},
			},
			"tx2": namespaceToWrites{
				1: &namespaceWrites{
					keys:     [][]byte{k5},
					values:   [][]byte{[]byte("v5")},
					versions: [][]byte{nil},
				},
				2: &namespaceWrites{
					keys:     [][]byte{k6, k7},
					values:   [][]byte{[]byte("v6"), nil},
					versions: [][]byte{nil, nil},
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
	require.Equal(t, expectedPreparedTxs.newWrites, preparedTxs.newWrites)
}

func TestPrepareTx(t *testing.T) {
	t.Parallel()

	env := newPrepareTestEnv(t)
	env.preparer.start(1)

	v1 := versionNumber(1).bytes()
	v2 := versionNumber(2).bytes()
	v3 := versionNumber(3).bytes()
	v4 := versionNumber(4).bytes()
	v8 := versionNumber(8).bytes()
	v9 := versionNumber(9).bytes()
	v10 := versionNumber(10).bytes()

	k1 := []byte("key1")
	k2 := []byte("key2")
	k3 := []byte("key3")
	k4 := []byte("key4")
	k5 := []byte("key5")
	k6 := []byte("key6")
	k7 := []byte("key7")
	k8 := []byte("key8")
	k9 := []byte("key9")
	k10 := []byte("key10")

	tx := &protovcservice.TransactionBatch{
		Transactions: []*protovcservice.Transaction{
			{
				ID: "tx1",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
						ReadsOnly: []*protoblocktx.Read{
							{Key: k1, Version: v1},
							{Key: k2, Version: v2},
						},
						ReadWrites: []*protoblocktx.ReadWrite{
							{Key: k3, Version: v3, Value: []byte("v3")},
						},
						BlindWrites: []*protoblocktx.Write{
							{Key: k4, Value: []byte("v4")},
						},
					},
					{
						NsId: 2,
						ReadsOnly: []*protoblocktx.Read{
							{Key: k5, Version: nil},
						},
						ReadWrites: []*protoblocktx.ReadWrite{
							{Key: k6, Version: nil, Value: []byte("v6")},
						},
						BlindWrites: []*protoblocktx.Write{
							{Key: k7, Value: []byte("v7")},
						},
					},
				},
			},
			{
				ID: "tx2",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
						ReadsOnly: []*protoblocktx.Read{
							{Key: k8, Version: v8},
						},
						ReadWrites: []*protoblocktx.ReadWrite{
							{Key: k9, Version: v9, Value: []byte("v9")},
						},
						BlindWrites: []*protoblocktx.Write{
							{Key: k10, Value: []byte("v10")},
						},
					},
				},
			},
		},
	}

	expectedPreparedTxs := &preparedTransactions{
		namespaceToReadEntries: namespaceToReads{
			1: &reads{
				keys:     [][]byte{k1, k2, k3, k8, k9},
				versions: [][]byte{v1, v2, v3, v8, v9},
			},
			2: &reads{
				keys:     [][]byte{k5},
				versions: [][]byte{nil},
			},
		},
		readToTransactionIndices: readToTransactions{
			comparableRead{1, string(k1), string(v1)}: []txID{"tx1"},
			comparableRead{1, string(k2), string(v2)}: []txID{"tx1"},
			comparableRead{1, string(k3), string(v3)}: []txID{"tx1"},
			comparableRead{1, string(k8), string(v8)}: []txID{"tx2"},
			comparableRead{1, string(k9), string(v9)}: []txID{"tx2"},
			comparableRead{2, string(k5), ""}:         []txID{"tx1"},
			comparableRead{2, string(k6), ""}:         []txID{"tx1"},
		},
		nonBlindWritesPerTransaction: transactionToWrites{
			"tx1": namespaceToWrites{
				1: &namespaceWrites{
					keys:     [][]byte{k3},
					values:   [][]byte{[]byte("v3")},
					versions: [][]byte{v4},
				},
			},
			"tx2": namespaceToWrites{
				1: &namespaceWrites{
					keys:     [][]byte{k9},
					values:   [][]byte{[]byte("v9")},
					versions: [][]byte{v10},
				},
			},
		},
		blindWritesPerTransaction: transactionToWrites{
			"tx1": namespaceToWrites{
				1: &namespaceWrites{
					keys:     [][]byte{k4},
					values:   [][]byte{[]byte("v4")},
					versions: [][]byte{nil},
				},
				2: &namespaceWrites{
					keys:     [][]byte{k7},
					values:   [][]byte{[]byte("v7")},
					versions: [][]byte{nil},
				},
			},
			"tx2": namespaceToWrites{
				1: &namespaceWrites{
					keys:     [][]byte{k10},
					values:   [][]byte{[]byte("v10")},
					versions: [][]byte{nil},
				},
			},
		},
		newWrites: transactionToWrites{
			"tx1": namespaceToWrites{
				2: &namespaceWrites{
					keys:     [][]byte{k6},
					values:   [][]byte{[]byte("v6")},
					versions: [][]byte{nil},
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
	require.Equal(t, expectedPreparedTxs.newWrites, preparedTxs.newWrites)
}
