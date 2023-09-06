package dependencygraph

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

func TestTransactionNode(t *testing.T) {
	t.Parallel()

	keys := makeTestKeys(t, 7)

	tx1Node := createTxNode(
		t,
		[][]byte{keys[0], keys[1]}, // readsOnly
		[][]byte{keys[2], keys[3]}, // readWrites
		[][]byte{keys[4], keys[5]}, // blindWrites
	)

	tx2Node := createTxNode(
		t,
		[][]byte{keys[2]},          // readsOnly
		[][]byte{keys[0]},          // readWrites
		[][]byte{keys[4], keys[6]}, // blindWrites
	)

	tx2DependsOnTx := transactionSet{
		tx1Node: nil,
	}
	tx2Node.addDependenciesAndUpdateDependents(tx2DependsOnTx)
	require.False(t, tx2Node.isDependencyFree())
	require.Equal(t, tx2DependsOnTx, tx2Node.dependsOnTxs)
	checkDependentTxs(
		t,
		transactionSet{ // expectedDependentTxs
			tx2Node: nil,
		},
		tx1Node.dependentTxs, // actualDependentTxs
	)

	tx3Node := createTxNode(
		t,
		[][]byte{keys[5]}, // readsOnly
		[][]byte{keys[3]}, // readWrites
		[][]byte{keys[6]}, // blindWrites
	)

	tx3DependsOnTx := transactionSet{
		tx1Node: nil,
		tx2Node: nil,
	}
	tx3Node.addDependenciesAndUpdateDependents(tx3DependsOnTx)
	require.False(t, tx2Node.isDependencyFree())
	require.Equal(t, tx3DependsOnTx, tx3Node.dependsOnTxs)
	checkDependentTxs(
		t,
		transactionSet{ // expectedDependentTxs
			tx2Node: nil,
			tx3Node: nil,
		},
		tx1Node.dependentTxs, // actualDependentTxs
	)
	checkDependentTxs(
		t,
		transactionSet{ // expectedDependentTxs
			tx3Node: nil,
		},
		tx2Node.dependentTxs, // actualDependentTxs
	)

	freedTxs := tx1Node.freeDependents()
	require.Equal(t, []*TransactionNode{tx2Node}, freedTxs)
	require.Len(t, tx2Node.dependsOnTxs, 0)
}

func createTxNode(t *testing.T, readOnly, readWrite, blindWrite [][]byte) *TransactionNode {
	txWithTxID := createTxWithTxID(t, readOnly, readWrite, blindWrite)
	txNode := newTransactionNode(txWithTxID)

	var expectedReads []string  // nolint:prealloc
	var expectedWrites []string // nolint:prealloc
	nsID := txWithTxID.tx.Namespaces[0].NsId

	for _, k := range readOnly {
		expectedReads = append(expectedReads, constructCompositeKey(nsID, k))
	}

	for _, k := range readWrite {
		ck := constructCompositeKey(nsID, k)
		expectedReads = append(expectedReads, ck)
		expectedWrites = append(expectedWrites, ck)
	}

	for _, k := range blindWrite {
		expectedWrites = append(expectedWrites, constructCompositeKey(nsID, k))
	}

	checkNewTxNode(
		t,
		txWithTxID,
		&readWriteKeys{
			expectedReads,
			expectedWrites,
		},
		txNode,
	)

	return txNode
}

func createTxWithTxID(_ *testing.T, readOnly, readWrite, blindWrite [][]byte) *transactionWithTxID {
	nsID := uint32(1)

	reads := make([]*protoblocktx.Read, len(readOnly))
	for i, k := range readOnly {
		reads[i] = &protoblocktx.Read{Key: k}
	}

	readWrites := make([]*protoblocktx.ReadWrite, len(readWrite))
	for i, k := range readWrite {
		readWrites[i] = &protoblocktx.ReadWrite{Key: k}
	}

	blindWrites := make([]*protoblocktx.Write, len(blindWrite))
	for i, k := range blindWrite {
		blindWrites[i] = &protoblocktx.Write{Key: k}
	}

	tx := &protoblocktx.Tx{
		Namespaces: []*protoblocktx.TxNamespace{
			{
				NsId:        nsID,
				ReadsOnly:   reads,
				ReadWrites:  readWrites,
				BlindWrites: blindWrites,
			},
		},
	}

	return &transactionWithTxID{
		txID: uuid.New().String(),
		tx:   tx,
	}
}

func checkNewTxNode(
	t *testing.T,
	txWithID *transactionWithTxID,
	readsWrites *readWriteKeys,
	txNode *TransactionNode,
) {
	require.Equal(t, txWithID.txID, txNode.Tx.ID)
	require.Equal(t, txWithID.tx.Namespaces, txNode.Tx.Namespaces)
	require.True(t, txNode.isDependencyFree())
	require.ElementsMatch(t, readsWrites.reads, txNode.rwKeys.reads)
	require.ElementsMatch(t, readsWrites.writes, txNode.rwKeys.writes)
	require.Equal(t, 0, getLengthOfDependentTx(t, txNode.dependentTxs))
}

func checkDependentTxs(t *testing.T, expectedTransactionList transactionSet, dependentTxs *sync.Map) {
	actualLen := getLengthOfDependentTx(t, dependentTxs)
	require.Len(t, expectedTransactionList, actualLen)

	actualTransactionList := make(transactionSet)
	dependentTxs.Range(func(k, _ any) bool {
		txNode, _ := k.(*TransactionNode)
		actualTransactionList[txNode] = nil
		return true
	})
	require.Equal(t, expectedTransactionList, actualTransactionList)
}

func getLengthOfDependentTx(_ *testing.T, dependentTxs *sync.Map) int {
	var length int
	dependentTxs.Range(func(_, _ any) bool {
		length++
		return true
	})

	return length
}
