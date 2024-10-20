package vcservice

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
)

type committerTestEnv struct {
	c            *transactionCommitter
	validatedTxs chan *validatedTransactions
	txStatus     chan *protovcservice.TransactionStatus
	dbEnv        *DatabaseTestEnv
}

func newCommitterTestEnv(t *testing.T) *committerTestEnv {
	validatedTxs := make(chan *validatedTransactions, 10)
	txStatus := make(chan *protovcservice.TransactionStatus, 10)

	dbEnv := NewDatabaseTestEnv(t)
	metrics := newVCServiceMetrics()
	c := newCommitter(dbEnv.DB, validatedTxs, txStatus, metrics)

	wg := sync.WaitGroup{}
	t.Cleanup(wg.Wait)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	wg.Add(1)
	go func() { require.NoError(t, c.run(ctx, 1)); wg.Done() }()

	return &committerTestEnv{
		c:            c,
		validatedTxs: validatedTxs,
		txStatus:     txStatus,
		dbEnv:        dbEnv,
	}
}

type state struct {
	namespace      types.NamespaceID
	keySuffix      int
	updateSequence int
}

func writes(isBlind bool, allWrites ...state) namespaceToWrites { // nolint: revive
	ntw := make(namespaceToWrites)
	for _, ww := range allWrites {
		nw := ntw.getOrCreate(ww.namespace)
		var ver []byte
		if !isBlind {
			ver = types.VersionNumber(ww.updateSequence).Bytes()
		}
		nw.append(
			[]byte(fmt.Sprintf("key%d.%d", ww.namespace, ww.keySuffix)),
			[]byte(fmt.Sprintf("value%d.%d.%d", ww.namespace, ww.keySuffix, ww.updateSequence)),
			ver,
		)
	}
	return ntw
}

func TestCommit(t *testing.T) {
	env := newCommitterTestEnv(t)

	env.dbEnv.populateDataWithCleanup(
		t,
		[]int{1, 2},
		writes(
			false,
			state{1, 1, 1},
			state{1, 2, 1},
			state{1, 3, 2},
			state{1, 4, 2},
			state{2, 1, 0},
			state{2, 2, 0},
			state{2, 3, 1},
			state{2, 4, 1},
		),
		&protovcservice.TransactionStatus{
			Status: map[string]protoblocktx.Status{
				"tx1": protoblocktx.Status_COMMITTED,
				"tx2": protoblocktx.Status_COMMITTED,
			},
		},
		transactionIDToHeight{
			"tx1": types.NewHeight(1, 1),
			"tx2": types.NewHeight(1, 2),
		},
	)

	// Note: the order of the subtest is important
	tests := []struct {
		name                      string
		txs                       *validatedTransactions
		expectedTxStatuses        map[string]protoblocktx.Status
		expectedNsRows            namespaceToWrites
		unexpectedNsRows          namespaceToWrites
		expectedMaxSeenBlocNumber uint64
	}{
		{
			name: "new writes",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{},
				validTxBlindWrites:    transactionToWrites{},
				newWrites: transactionToWrites{
					"tx-new-1": writes(
						true,
						state{1, 10, 0},
						state{1, 11, 0},
						state{2, 10, 0},
						state{2, 11, 0},
					),
					"tx-new-2": writes(
						true,
						state{1, 20, 0},
						state{1, 21, 0},
						state{2, 20, 0},
						state{2, 21, 0},
					),
				},
				invalidTxStatus: map[TxID]protoblocktx.Status{},
				txIDToHeight: transactionIDToHeight{
					"tx-new-1": types.NewHeight(1, 1),
					"tx-new-2": types.NewHeight(244, 2),
				},
			},
			expectedTxStatuses: map[string]protoblocktx.Status{
				"tx-new-1": protoblocktx.Status_COMMITTED,
				"tx-new-2": protoblocktx.Status_COMMITTED,
			},
			expectedNsRows: writes(
				false,
				state{1, 10, 0},
				state{1, 11, 0},
				state{2, 10, 0},
				state{2, 11, 0},
				state{1, 20, 0},
				state{1, 21, 0},
				state{2, 20, 0},
				state{2, 21, 0},
			),
			expectedMaxSeenBlocNumber: 244,
		},
		{
			name: "non-blind writes",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx-non-blind-1": writes(
						false,
						state{1, 1, 2},
						state{1, 2, 2},
						state{2, 1, 1},
						state{2, 2, 1},
					),
				},
				validTxBlindWrites: transactionToWrites{},
				newWrites:          transactionToWrites{},
				invalidTxStatus:    map[TxID]protoblocktx.Status{},
				txIDToHeight: transactionIDToHeight{
					"tx-non-blind-1": types.NewHeight(239, 1),
				},
			},
			expectedTxStatuses: map[string]protoblocktx.Status{"tx-non-blind-1": protoblocktx.Status_COMMITTED},
			expectedNsRows: writes(
				false,
				state{1, 1, 2},
				state{1, 2, 2},
				state{2, 1, 1},
				state{2, 2, 1},
			),
			expectedMaxSeenBlocNumber: 244,
		},
		{
			name: "blind writes",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{},
				validTxBlindWrites: transactionToWrites{
					"tx-blind-1": writes(
						true,
						state{1, 1, 3},
						state{1, 2, 3},
						state{2, 1, 2},
						state{2, 2, 2},
					),
				},
				newWrites:       transactionToWrites{},
				invalidTxStatus: map[TxID]protoblocktx.Status{},
				txIDToHeight: transactionIDToHeight{
					"tx-blind-1": types.NewHeight(1024, 1),
				},
			},
			expectedTxStatuses: map[string]protoblocktx.Status{
				"tx-blind-1": protoblocktx.Status_COMMITTED,
			},
			expectedNsRows: writes(
				false,
				state{1, 1, 3},
				state{1, 2, 3},
				state{2, 1, 2},
				state{2, 2, 2},
			),
			expectedMaxSeenBlocNumber: 1024,
		},
		{
			name: "blind, non-blind, and new writes",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx-all-1": writes(
						false,
						state{1, 1, 4},
						state{1, 2, 4},
					),
					"tx-all-2": writes(
						false,
						state{1, 3, 3},
						state{1, 4, 3},
					),
				},
				validTxBlindWrites: transactionToWrites{
					"tx-all-1": writes(
						true,
						state{2, 1, 3},
						state{2, 2, 3},
						state{2, 5, 0},
					),
					"tx-all-2": writes(
						true,
						state{2, 3, 2},
						state{2, 4, 2},
						state{2, 6, 0},
					),
				},
				newWrites: transactionToWrites{
					"tx-all-1": writes(
						true,
						state{2, 30, 0},
					),
					"tx-all-2": writes(
						true,
						state{2, 31, 0},
					),
				},
				invalidTxStatus: map[TxID]protoblocktx.Status{
					"tx-conflict-1": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
					"tx-conflict-2": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
					"tx-conflict-3": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				},
				txIDToHeight: transactionIDToHeight{
					"tx-all-1":      types.NewHeight(5, 1),
					"tx-all-2":      types.NewHeight(200, 2),
					"tx-conflict-1": types.NewHeight(1, 1),
					"tx-conflict-2": types.NewHeight(396, 2),
					"tx-conflict-3": types.NewHeight(396, 3),
				},
			},
			expectedTxStatuses: map[string]protoblocktx.Status{
				"tx-all-1":      protoblocktx.Status_COMMITTED,
				"tx-all-2":      protoblocktx.Status_COMMITTED,
				"tx-conflict-1": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				"tx-conflict-2": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				"tx-conflict-3": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
			},
			expectedNsRows: writes(
				false,
				state{1, 1, 4},
				state{1, 2, 4},
				state{1, 3, 3},
				state{1, 4, 3},
				state{2, 1, 3},
				state{2, 2, 3},
				state{2, 3, 2},
				state{2, 4, 2},
				state{2, 5, 0},
				state{2, 6, 0},
				state{2, 30, 0},
				state{2, 31, 0},
			),
			expectedMaxSeenBlocNumber: 1024,
		},
		{
			name: "new writes with violating",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx-not-violate-1": writes(
						false,
						state{1, 1, 5},
					),
					"tx-violate-1": writes(
						true,
						state{1, 3, 4},
					),
				},
				validTxBlindWrites: transactionToWrites{
					"tx-not-violate-1": writes(
						false,
						state{1, 2, 5},
					),
					"tx-violate-1": writes(
						true,
						state{1, 4, 4},
					),
				},
				newWrites: transactionToWrites{
					"tx-not-violate-1": writes(
						true,
						state{1, 22, 0}, // not violate
					),
					"tx-violate-1": writes(
						true,
						state{1, 10, 0}, // violate
						state{1, 12, 0}, // not violate
					),
				},
				invalidTxStatus: map[TxID]protoblocktx.Status{
					"tx-conflict-4": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				},
				readToTransactionIndices: map[comparableRead][]TxID{
					{1, "key1.10", ""}: {"tx-violate-1"},
				},
				txIDToHeight: transactionIDToHeight{
					"tx-violate-1":     types.NewHeight(1, 1),
					"tx-not-violate-1": types.NewHeight(4, 2),
					"tx-conflict-4":    types.NewHeight(1000, 3),
				},
			},
			expectedTxStatuses: map[string]protoblocktx.Status{
				"tx-violate-1":     protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				"tx-not-violate-1": protoblocktx.Status_COMMITTED,
				"tx-conflict-4":    protoblocktx.Status_ABORTED_MVCC_CONFLICT,
			},
			expectedNsRows: writes(
				false,
				state{1, 1, 5},
				state{1, 2, 5},
				state{1, 3, 3},
				state{1, 4, 3},
				state{1, 10, 0},
				state{1, 22, 0},
			),
			unexpectedNsRows: writes(
				false,
				state{1, 12, 0},
			),
			expectedMaxSeenBlocNumber: 10000,
		},
		{
			name: "all invalid txs",
			txs: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx1": writes(false, state{2, 7, 1}),
				},
				validTxBlindWrites: transactionToWrites{
					"tx1": writes(true, state{1, 1, 6}),
				},
				newWrites: transactionToWrites{
					"tx1": writes(true, state{1, 40, 0}),
				},
				invalidTxStatus: map[TxID]protoblocktx.Status{
					"tx-conflict-10": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
					"tx-conflict-11": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
					"tx-conflict-12": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				},
				txIDToHeight: transactionIDToHeight{
					"tx1":            types.NewHeight(1, 5),
					"tx-conflict-10": types.NewHeight(1, 1),
					"tx-conflict-11": types.NewHeight(4, 2),
					"tx-conflict-12": types.NewHeight(66000, 3),
				},
			},
			expectedTxStatuses: map[string]protoblocktx.Status{
				"tx1":            protoblocktx.Status_ABORTED_DUPLICATE_TXID,
				"tx-conflict-10": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				"tx-conflict-11": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				"tx-conflict-12": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
			},
			expectedNsRows: writes(
				false,
				state{1, 1, 5},
			),
			unexpectedNsRows: writes(
				false,
				state{2, 7, 0},
				state{1, 40, 0},
			),
			expectedMaxSeenBlocNumber: 66000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.validatedTxs <- tt.txs
			txStatus := <-env.txStatus
			require.Equal(t, &protovcservice.TransactionStatus{Status: tt.expectedTxStatuses}, txStatus)
			for nsID, expectedRows := range tt.expectedNsRows {
				env.dbEnv.rowExists(t, nsID, *expectedRows)
			}
			for nsID, expectedRows := range tt.unexpectedNsRows {
				env.dbEnv.rowNotExists(t, nsID, expectedRows.keys)
			}
			env.dbEnv.StatusExistsForNonDuplicateTxID(t, tt.expectedTxStatuses, tt.txs.txIDToHeight)
		})
	}
}
