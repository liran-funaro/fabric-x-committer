/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package dependencygraph

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

var nsID1ForTest = "1"

// TestTransactionNodeSystemNamespaces verifies that system namespaces do not take the
// normal _meta namespace-version read dependency, mirroring the existing _meta/_config
// exemption for _snapshot and _checkpoint.
func TestTransactionNodeSystemNamespaces(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		nsID string
	}{
		{name: "meta namespace", nsID: committerpb.MetaNamespaceID},
		{name: "config namespace", nsID: committerpb.ConfigNamespaceID},
		{name: "snapshot namespace", nsID: committerpb.SnapshotNamespaceID},
		{name: "checkpoint namespace", nsID: committerpb.CheckpointNamespaceID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tx := createTxForTest(t, 0, tc.nsID, nil, [][]byte{[]byte("k")}, nil)
			txNode := newTransactionNode(tx)

			metaKey := constructCompositeKey(committerpb.MetaNamespaceID, []byte(tc.nsID))
			require.NotContains(t, txNode.rwKeys.readsOnly, metaKey,
				"system namespace %s must not take the _meta namespace-version read dependency", tc.nsID)
		})
	}
}

// TestTransactionNodeApplicationNamespace verifies that an ordinary application namespace
// still takes the _meta namespace-version read dependency.
func TestTransactionNodeApplicationNamespace(t *testing.T) {
	t.Parallel()
	tx := createTxForTest(t, 0, nsID1ForTest, nil, [][]byte{[]byte("k")}, nil)
	txNode := newTransactionNode(tx)

	metaKey := constructCompositeKey(committerpb.MetaNamespaceID, []byte(nsID1ForTest))
	require.Contains(t, txNode.rwKeys.readsOnly, metaKey)
}

// TestTransactionNodeMetaWriteSupersedesRead requires that a namespace lifecycle transaction does not
// contribute the same composite key as both a writer and a reader of itself.
//
// Every non-system namespace in a transaction gets a reads-only dependency on _meta:<ns>, and a
// lifecycle transaction changing that namespace's policy writes precisely that key through the _meta
// namespace. Contributed twice, the simple manager queues the transaction behind the running group its
// own write created and never releases it. Per-namespace duplicate-key validation cannot see this,
// because the two contributions come from two different namespaces of the same transaction.
func TestTransactionNodeMetaWriteSupersedesRead(t *testing.T) {
	t.Parallel()
	txNode := newTransactionNode(createNamespaceLifecycleTxForTest(nsID1ForTest, []byte("k")))

	metaKey := constructCompositeKey(committerpb.MetaNamespaceID, []byte(nsID1ForTest))
	require.Contains(t, txNode.rwKeys.readsAndWrites, metaKey,
		"the lifecycle write of the namespace's _meta entry must be kept")
	require.NotContains(t, txNode.rwKeys.readsOnly, metaKey,
		"the same transaction must not also read the _meta key it writes")
}

// createNamespaceLifecycleTxForTest builds the transaction shape that reproduces the self-dependency:
// a read-write on ns through the _meta namespace, which is how a policy change is expressed, plus a
// blind write inside ns itself.
func createNamespaceLifecycleTxForTest(nsID string, key []byte) *servicepb.TxWithRef {
	return &servicepb.TxWithRef{
		Ref: committerpb.NewTxRef(uuid.New().String(), 0, 0),
		Content: &applicationpb.Tx{
			Namespaces: []*applicationpb.TxNamespace{
				{
					NsId:       committerpb.MetaNamespaceID,
					ReadWrites: []*applicationpb.ReadWrite{{Key: []byte(nsID)}},
				},
				{
					NsId:        nsID,
					BlindWrites: []*applicationpb.Write{{Key: key}},
				},
			},
		},
	}
}

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

	tx2DependsOnTx := TxNodeBatch{
		tx1Node,
	}
	tx2Node.addDependenciesAndUpdateDependents(tx2DependsOnTx)
	require.False(t, tx2Node.isDependencyFree())
	require.Equal(t, tx2DependsOnTx, tx2Node.dependsOnTxs)
	checkDependentTxs(
		t,
		TxNodeBatch{ // expectedDependentTxs
			tx2Node,
		},
		&tx1Node.dependentTxs, // actualDependentTxs
	)

	tx3Node := createTxNode(
		t,
		[][]byte{keys[5]}, // readsOnly
		[][]byte{keys[3]}, // readWrites
		[][]byte{keys[6]}, // blindWrites
	)

	tx3DependsOnTx := TxNodeBatch{
		tx1Node,
		tx2Node,
	}
	tx3Node.addDependenciesAndUpdateDependents(tx3DependsOnTx)
	require.False(t, tx2Node.isDependencyFree())
	require.Equal(t, tx3DependsOnTx, tx3Node.dependsOnTxs)
	checkDependentTxs(
		t,
		TxNodeBatch{ // expectedDependentTxs
			tx2Node,
			tx3Node,
		},
		&tx1Node.dependentTxs, // actualDependentTxs
	)
	checkDependentTxs(
		t,
		TxNodeBatch{ // expectedDependentTxs
			tx3Node,
		},
		&tx2Node.dependentTxs, // actualDependentTxs
	)

	freedTxs := tx1Node.freeDependents()
	require.Equal(t, TxNodeBatch{tx2Node}, freedTxs)
	require.Empty(t, tx2Node.dependsOnTxs)
}

func createTxNode(t *testing.T, readOnly, readWrite, blindWrite [][]byte) *TransactionNode {
	t.Helper()
	tx := createTxForTest(t, 0, nsID1ForTest, readOnly, readWrite, blindWrite)
	txNode := newTransactionNode(tx)

	expectedReads := make([]string, 0, len(readOnly))
	expectedWrites := make([]string, 0, len(blindWrite))
	expectedReadsAndWrites := make([]string, 0, len(readWrite))

	for _, k := range readOnly {
		expectedReads = append(expectedReads, constructCompositeKey(nsID1ForTest, k))
	}
	expectedReads = append(
		expectedReads,
		constructCompositeKey(committerpb.MetaNamespaceID, []byte(nsID1ForTest)),
	)

	for _, k := range readWrite {
		expectedReadsAndWrites = append(expectedReadsAndWrites, constructCompositeKey(nsID1ForTest, k))
	}

	for _, k := range blindWrite {
		expectedWrites = append(expectedWrites, constructCompositeKey(nsID1ForTest, k))
	}

	checkNewTxNode(
		t,
		tx,
		&readWriteKeys{
			expectedReads,
			expectedWrites,
			expectedReadsAndWrites,
		},
		txNode,
	)

	return txNode
}

func createTxForTest( //nolint: revive
	_ *testing.T, txNum int, nsID string, readOnly, readWrite, blindWrite [][]byte,
) *servicepb.TxWithRef {
	reads := make([]*applicationpb.Read, len(readOnly))
	for i, k := range readOnly {
		reads[i] = &applicationpb.Read{Key: k}
	}

	readWrites := make([]*applicationpb.ReadWrite, len(readWrite))
	for i, k := range readWrite {
		readWrites[i] = &applicationpb.ReadWrite{Key: k}
	}

	blindWrites := make([]*applicationpb.Write, len(blindWrite))
	for i, k := range blindWrite {
		blindWrites[i] = &applicationpb.Write{Key: k}
	}

	return &servicepb.TxWithRef{
		Ref: committerpb.NewTxRef(uuid.New().String(), 0, uint32(txNum)), //nolint:gosec // int -> uint32.
		Content: &applicationpb.Tx{
			Namespaces: []*applicationpb.TxNamespace{{
				NsId:        nsID,
				ReadsOnly:   reads,
				ReadWrites:  readWrites,
				BlindWrites: blindWrites,
			}},
		},
	}
}

func checkNewTxNode(
	t *testing.T,
	tx *servicepb.TxWithRef,
	readsWrites *readWriteKeys,
	txNode *TransactionNode,
) {
	t.Helper()
	test.RequireProtoEqual(t, tx.Ref, txNode.VCTx.Ref)
	test.RequireProtoElementsMatch(t, tx.Content.Namespaces, txNode.VCTx.Namespaces)
	require.True(t, txNode.isDependencyFree())
	require.ElementsMatch(t, readsWrites.readsOnly, txNode.rwKeys.readsOnly)
	require.ElementsMatch(t, readsWrites.writesOnly, txNode.rwKeys.writesOnly)
	require.Equal(t, 0, txNode.dependentTxs.Count())
}

func checkDependentTxs(
	t *testing.T, expectedTransactionList TxNodeBatch, dependentTxs *utils.SyncMap[*TransactionNode, any],
) {
	t.Helper()
	actualTransactionList := slices.Collect(dependentTxs.IterKeys())
	require.Len(t, expectedTransactionList, len(actualTransactionList))
	require.ElementsMatch(t, expectedTransactionList, actualTransactionList)
}
