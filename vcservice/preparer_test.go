package vcservice

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
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
						NsId:      1,
						NsVersion: v1,
						ReadsOnly: []*protoblocktx.Read{
							{Key: k1, Version: v1},
							{Key: k2, Version: v1},
							{Key: k3, Version: nil},
						},
					},
					{
						NsId:      2,
						NsVersion: v1,
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
						NsId:      1,
						NsVersion: v1,
						ReadsOnly: []*protoblocktx.Read{
							{Key: k1, Version: v1},
							{Key: k4, Version: v1},
							{Key: k5, Version: nil},
						},
					},
					{
						NsId:      2,
						NsVersion: v1,
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
		nsToReads: namespaceToReads{
			1: &reads{
				keys:     [][]byte{k1, k2, k3, k4, k5},
				versions: [][]byte{v1, v1, nil, v1, nil},
			},
			2: &reads{
				keys:     [][]byte{k4, k5, k4, k5, k6},
				versions: [][]byte{v0, nil, v1, v0, nil},
			},
			types.MetaNamespaceID: &reads{
				keys:     [][]byte{types.NamespaceID(1).Bytes(), types.NamespaceID(2).Bytes()},
				versions: [][]byte{v1, v1},
			},
		},
		readToTxIDs: readToTransactions{
			comparableRead{1, string(k1), string(v1)}: []txID{
				"tx1", "tx2",
			},
			comparableRead{1, string(k2), string(v1)}: []txID{"tx1"},
			comparableRead{1, string(k3), ""}:         []txID{"tx1"},
			comparableRead{1, string(k4), string(v1)}: []txID{"tx2"},
			comparableRead{1, string(k5), ""}:         []txID{"tx2"},
			comparableRead{2, string(k4), string(v0)}: []txID{"tx1"},
			comparableRead{2, string(k5), ""}:         []txID{"tx1"},
			comparableRead{2, string(k4), string(v1)}: []txID{"tx2"},
			comparableRead{2, string(k5), string(v0)}: []txID{"tx2"},
			comparableRead{2, string(k6), ""}:         []txID{"tx2"},
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(1).Bytes()), string(v1)}: []txID{
				"tx1", "tx2",
			},
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(2).Bytes()), string(v1)}: []txID{
				"tx1", "tx2",
			},
		},
		txIDToNsNonBlindWrites: transactionToWrites{},
		txIDToNsBlindWrites:    transactionToWrites{},
	}

	env.txBatch <- tx
	preparedTxs := <-env.preparedTxs
	require.Equal(t, expectedPreparedTxs.nsToReads, preparedTxs.nsToReads)
	require.Equal(t, expectedPreparedTxs.readToTxIDs, preparedTxs.readToTxIDs)
	require.Equal(t, expectedPreparedTxs.txIDToNsNonBlindWrites, preparedTxs.txIDToNsNonBlindWrites)
	require.Equal(t, expectedPreparedTxs.txIDToNsBlindWrites, preparedTxs.txIDToNsBlindWrites)
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

	v2 := types.VersionNumber(2).Bytes()

	tx := &protovcservice.TransactionBatch{
		Transactions: []*protovcservice.Transaction{
			{
				ID: "tx1",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      1,
						NsVersion: v1,
						BlindWrites: []*protoblocktx.Write{
							{Key: k1, Value: []byte("1")},
							{Key: k2, Value: []byte("1")},
							{Key: k3, Value: nil},
						},
					},
					{
						NsId:      2,
						NsVersion: v1,
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
						NsId:      1,
						NsVersion: v2,
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
		nsToReads: namespaceToReads{
			types.MetaNamespaceID: &reads{
				keys: [][]byte{
					types.NamespaceID(1).Bytes(), types.NamespaceID(2).Bytes(), types.NamespaceID(1).Bytes(),
				},
				versions: [][]byte{v1, v1, v2},
			},
		},
		readToTxIDs: readToTransactions{
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(1).Bytes()), string(v1)}: []txID{"tx1"},
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(2).Bytes()), string(v1)}: []txID{"tx1"},
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(1).Bytes()), string(v2)}: []txID{"tx2"},
		},
		txIDToNsNonBlindWrites: transactionToWrites{},
		txIDToNsBlindWrites: transactionToWrites{
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
	require.Equal(t, expectedPreparedTxs.nsToReads, preparedTxs.nsToReads)
	require.Equal(t, expectedPreparedTxs.readToTxIDs, preparedTxs.readToTxIDs)
	require.Equal(t, expectedPreparedTxs.txIDToNsNonBlindWrites, preparedTxs.txIDToNsNonBlindWrites)
	require.Equal(t, expectedPreparedTxs.txIDToNsBlindWrites, preparedTxs.txIDToNsBlindWrites)
}

func TestPrepareTxWithReadWritesOnly(t *testing.T) {
	t.Parallel()

	env := newPrepareTestEnv(t)
	env.preparer.start(1)

	v2 := types.VersionNumber(2).Bytes()

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
						NsId:      1,
						NsVersion: v2,
						ReadWrites: []*protoblocktx.ReadWrite{
							{Key: k1, Version: v1, Value: []byte("v1")},
							{Key: k2, Version: v1, Value: []byte("v2")},
							{Key: k3, Version: nil, Value: []byte("v3")},
						},
					},
					{
						NsId:      2,
						NsVersion: v2,
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
						NsId:      1,
						NsVersion: v2,
						ReadWrites: []*protoblocktx.ReadWrite{
							{Key: k4, Version: v1, Value: []byte("v4")},
							{Key: k5, Version: nil, Value: []byte("v5")},
						},
					},
					{
						NsId:      2,
						NsVersion: v2,
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
		nsToReads: namespaceToReads{
			1: &reads{
				keys:     [][]byte{k1, k2, k4},
				versions: [][]byte{v1, v1, v1},
			},
			2: &reads{
				keys:     [][]byte{k4},
				versions: [][]byte{v0},
			},
			types.MetaNamespaceID: &reads{
				keys:     [][]byte{types.NamespaceID(1).Bytes(), types.NamespaceID(2).Bytes()},
				versions: [][]byte{v2, v2},
			},
		},
		readToTxIDs: readToTransactions{
			comparableRead{1, string(k1), string(v1)}: []txID{"tx1"},
			comparableRead{1, string(k2), string(v1)}: []txID{"tx1"},
			comparableRead{1, string(k3), ""}:         []txID{"tx1"},
			comparableRead{1, string(k4), string(v1)}: []txID{"tx2"},
			comparableRead{1, string(k5), ""}:         []txID{"tx2"},
			comparableRead{2, string(k4), string(v0)}: []txID{"tx1"},
			comparableRead{2, string(k5), ""}:         []txID{"tx1"},
			comparableRead{2, string(k6), ""}:         []txID{"tx2"},
			comparableRead{2, string(k7), ""}:         []txID{"tx2"},
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(1).Bytes()), string(v2)}: []txID{
				"tx1", "tx2",
			},
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(2).Bytes()), string(v2)}: []txID{
				"tx1", "tx2",
			},
		},
		txIDToNsNonBlindWrites: transactionToWrites{
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
		txIDToNsBlindWrites: transactionToWrites{},
		txIDToNsNewWrites: transactionToWrites{
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
	require.Equal(t, expectedPreparedTxs.nsToReads, preparedTxs.nsToReads)
	require.Equal(t, expectedPreparedTxs.readToTxIDs, preparedTxs.readToTxIDs)
	require.Equal(t, expectedPreparedTxs.txIDToNsNonBlindWrites, preparedTxs.txIDToNsNonBlindWrites)
	require.Equal(t, expectedPreparedTxs.txIDToNsBlindWrites, preparedTxs.txIDToNsBlindWrites)
	require.Equal(t, expectedPreparedTxs.txIDToNsNewWrites, preparedTxs.txIDToNsNewWrites)
}

func TestPrepareTx(t *testing.T) {
	t.Parallel()

	env := newPrepareTestEnv(t)
	env.preparer.start(1)

	v2 := types.VersionNumber(2).Bytes()
	v3 := types.VersionNumber(3).Bytes()
	v4 := types.VersionNumber(4).Bytes()
	v8 := types.VersionNumber(8).Bytes()
	v9 := types.VersionNumber(9).Bytes()
	v10 := types.VersionNumber(10).Bytes()

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
						NsId:      1,
						NsVersion: v1,
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
						NsId:      2,
						NsVersion: v2,
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
						NsId:      1,
						NsVersion: v3,
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
			{
				ID: "tx3",
				PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
					Code: protoblocktx.Status_ABORTED_NO_WRITES,
				},
			},
			{
				ID: "tx4",
				PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
					Code: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
				},
			},
		},
	}

	expectedPreparedTxs := &preparedTransactions{
		nsToReads: namespaceToReads{
			1: &reads{
				keys:     [][]byte{k1, k2, k3, k8, k9},
				versions: [][]byte{v1, v2, v3, v8, v9},
			},
			2: &reads{
				keys:     [][]byte{k5},
				versions: [][]byte{nil},
			},
			types.MetaNamespaceID: &reads{
				keys: [][]byte{
					types.NamespaceID(1).Bytes(), types.NamespaceID(2).Bytes(), types.NamespaceID(1).Bytes(),
				},
				versions: [][]byte{v1, v2, v3},
			},
		},
		readToTxIDs: readToTransactions{
			comparableRead{1, string(k1), string(v1)}:                                               []txID{"tx1"},
			comparableRead{1, string(k2), string(v2)}:                                               []txID{"tx1"},
			comparableRead{1, string(k3), string(v3)}:                                               []txID{"tx1"},
			comparableRead{1, string(k8), string(v8)}:                                               []txID{"tx2"},
			comparableRead{1, string(k9), string(v9)}:                                               []txID{"tx2"},
			comparableRead{2, string(k5), ""}:                                                       []txID{"tx1"},
			comparableRead{2, string(k6), ""}:                                                       []txID{"tx1"},
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(1).Bytes()), string(v1)}: []txID{"tx1"},
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(2).Bytes()), string(v2)}: []txID{"tx1"},
			comparableRead{types.MetaNamespaceID, string(types.NamespaceID(1).Bytes()), string(v3)}: []txID{"tx2"},
		},
		txIDToNsNonBlindWrites: transactionToWrites{
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
		txIDToNsBlindWrites: transactionToWrites{
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
		txIDToNsNewWrites: transactionToWrites{
			"tx1": namespaceToWrites{
				2: &namespaceWrites{
					keys:     [][]byte{k6},
					values:   [][]byte{[]byte("v6")},
					versions: [][]byte{nil},
				},
			},
		},
		invalidTxIDStatus: map[txID]protoblocktx.Status{
			"tx3": protoblocktx.Status_ABORTED_NO_WRITES,
			"tx4": protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
		},
	}

	env.txBatch <- tx
	preparedTxs := <-env.preparedTxs
	require.Equal(t, expectedPreparedTxs.nsToReads, preparedTxs.nsToReads)
	require.Equal(t, expectedPreparedTxs.readToTxIDs, preparedTxs.readToTxIDs)
	require.Equal(t, expectedPreparedTxs.txIDToNsNonBlindWrites, preparedTxs.txIDToNsNonBlindWrites)
	require.Equal(t, expectedPreparedTxs.txIDToNsBlindWrites, preparedTxs.txIDToNsBlindWrites)
	require.Equal(t, expectedPreparedTxs.txIDToNsNewWrites, preparedTxs.txIDToNsNewWrites)
	require.Equal(t, expectedPreparedTxs.invalidTxIDStatus, preparedTxs.invalidTxIDStatus)
}
