package vcservice

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

type validatorTestEnv struct {
	v            *transactionValidator
	preparedTxs  chan *preparedTransactions
	validatedTxs chan *validatedTransactions
	dbEnv        *databaseTestEnv
}

func newValidatorTestEnv(t *testing.T) *validatorTestEnv {
	preparedTxs := make(chan *preparedTransactions, 10)
	validatedTxs := make(chan *validatedTransactions, 10)

	dbEnv := newDatabaseTestEnv(t)
	v := newValidator(dbEnv.db, preparedTxs, validatedTxs, nil)

	t.Cleanup(func() {
		close(preparedTxs)
		close(validatedTxs)
	})

	return &validatorTestEnv{
		v:            v,
		preparedTxs:  preparedTxs,
		validatedTxs: validatedTxs,
		dbEnv:        dbEnv,
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	env := newValidatorTestEnv(t)
	env.v.start(1)

	v0 := versionNumber(0).bytes()
	v1 := versionNumber(1).bytes()
	v2 := versionNumber(2).bytes()

	k1_1 := []byte("key1.1")
	k1_2 := []byte("key1.2")
	k1_3 := []byte("key1.3")
	k1_4 := []byte("key1.4")
	k1_5 := []byte("key1.5")
	k1_6 := []byte("key1.6")
	k2_1 := []byte("key2.1")
	k2_2 := []byte("key2.2")
	k2_3 := []byte("key2.3")
	k2_4 := []byte("key2.4")
	k2_5 := []byte("key2.5")

	env.dbEnv.populateDataWithCleanup(
		t,
		[]int{1, 2},
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
		},
		nil,
	)

	tx1NonBlindWrites := namespaceToWrites{
		1: {
			keys:     [][]byte{k1_1},
			values:   [][]byte{[]byte("value1.1.1")},
			versions: [][]byte{v2},
		},
		2: {
			keys:     [][]byte{k2_1},
			values:   [][]byte{[]byte("value1.1.1")},
			versions: [][]byte{v2},
		},
	}
	tx2NonBlindWrites := namespaceToWrites{
		1: {
			keys:     [][]byte{k1_5},
			values:   [][]byte{[]byte("value1.5.1")},
			versions: [][]byte{v0},
		},
	}
	tx3NonBlindWrites := namespaceToWrites{
		2: {
			keys:     [][]byte{k2_2},
			values:   [][]byte{[]byte("value2.2.1")},
			versions: [][]byte{v2},
		},
	}
	tx3BlindWrites := namespaceToWrites{
		1: {
			keys:     [][]byte{k1_6},
			values:   [][]byte{[]byte("value1.6")},
			versions: [][]byte{nil},
		},
	}

	// Note: the order of the sub-test is important
	tests := []struct {
		name                string
		preparedTx          *preparedTransactions
		expectedValidatedTx *validatedTransactions
	}{
		{
			name: "all valid tx",
			preparedTx: &preparedTransactions{
				namespaceToReadEntries: namespaceToReads{
					1: &reads{
						keys:     [][]byte{k1_1, k1_2, k1_5},
						versions: [][]byte{v1, v1, nil},
					},
					2: &reads{
						keys:     [][]byte{k2_1, k2_2, k2_5},
						versions: [][]byte{v0, v0, nil},
					},
				},
				readToTransactionIndices: readToTransactions{
					comparableRead{1, string(k1_1), string(v1)}: []TxID{"tx1"},
					comparableRead{1, string(k1_2), string(v1)}: []TxID{"tx1"},
					comparableRead{1, string(k1_5), ""}:         []TxID{"tx2"},
					comparableRead{2, string(k2_1), string(v0)}: []TxID{"tx1"},
					comparableRead{2, string(k2_2), string(v0)}: []TxID{"tx3"},
					comparableRead{2, string(k2_5), ""}:         []TxID{"tx3"},
				},
				nonBlindWritesPerTransaction: transactionToWrites{
					"tx1": tx1NonBlindWrites,
					"tx2": tx2NonBlindWrites,
					"tx3": tx3NonBlindWrites,
				},
				blindWritesPerTransaction: transactionToWrites{
					"tx3": tx3BlindWrites,
				},
			},
			expectedValidatedTx: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx1": tx1NonBlindWrites,
					"tx2": tx2NonBlindWrites,
					"tx3": tx3NonBlindWrites,
				},
				validTxBlindWrites: transactionToWrites{
					"tx3": tx3BlindWrites,
				},
				invalidTxIndices: map[TxID]protoblocktx.Status{},
			},
		},
		{
			name: "all invalid tx",
			preparedTx: &preparedTransactions{
				namespaceToReadEntries: namespaceToReads{
					1: &reads{
						keys:     [][]byte{k1_1, k1_2, k1_5},
						versions: [][]byte{v0, v0, v1},
					},
					2: &reads{
						keys:     [][]byte{k2_1, k2_2, k2_5},
						versions: [][]byte{nil, nil, nil},
					},
				},
				readToTransactionIndices: readToTransactions{
					comparableRead{1, string(k1_1), string(v0)}: []TxID{"tx1"},
					comparableRead{1, string(k1_2), string(v0)}: []TxID{"tx1"},
					comparableRead{1, string(k1_5), string(v1)}: []TxID{"tx2"},
					comparableRead{2, string(k2_1), ""}:         []TxID{"tx1"},
					comparableRead{2, string(k2_2), ""}:         []TxID{"tx3"},
					comparableRead{2, string(k2_5), ""}:         []TxID{"tx3"},
				},
				nonBlindWritesPerTransaction: transactionToWrites{
					"tx1": tx1NonBlindWrites,
					"tx2": tx2NonBlindWrites,
					"tx3": tx3NonBlindWrites,
				},
				blindWritesPerTransaction: transactionToWrites{
					"tx3": tx3BlindWrites,
				},
			},
			expectedValidatedTx: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{},
				validTxBlindWrites:    transactionToWrites{},
				invalidTxIndices: map[TxID]protoblocktx.Status{
					"tx1": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
					"tx2": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
					"tx3": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				},
			},
		},
		{
			name: "valid and invalid tx",
			preparedTx: &preparedTransactions{
				namespaceToReadEntries: namespaceToReads{
					1: &reads{
						keys:     [][]byte{k1_1, k1_2, k1_5},
						versions: [][]byte{v1, v1, nil},
					},
					2: &reads{
						keys:     [][]byte{k2_1, k2_2, k2_5},
						versions: [][]byte{nil, nil, nil},
					},
				},
				readToTransactionIndices: readToTransactions{
					comparableRead{1, string(k1_1), string(v1)}: []TxID{"tx1"},
					comparableRead{1, string(k1_2), string(v1)}: []TxID{"tx1"},
					comparableRead{1, string(k1_5), ""}:         []TxID{"tx2"},
					comparableRead{2, string(k2_1), ""}:         []TxID{"tx1"},
					comparableRead{2, string(k2_2), ""}:         []TxID{"tx3"},
					comparableRead{2, string(k2_5), ""}:         []TxID{"tx3"},
				},
				nonBlindWritesPerTransaction: transactionToWrites{
					"tx1": tx1NonBlindWrites,
					"tx2": tx2NonBlindWrites,
					"tx3": tx3NonBlindWrites,
				},
				blindWritesPerTransaction: transactionToWrites{
					"tx3": tx3BlindWrites,
				},
			},
			expectedValidatedTx: &validatedTransactions{
				validTxNonBlindWrites: transactionToWrites{
					"tx2": tx2NonBlindWrites,
				},
				validTxBlindWrites: transactionToWrites{},
				invalidTxIndices: map[TxID]protoblocktx.Status{
					"tx1": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
					"tx3": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.preparedTxs <- tt.preparedTx
			validatedTxs := <-env.validatedTxs
			require.Equal(t, tt.expectedValidatedTx.validTxNonBlindWrites, validatedTxs.validTxNonBlindWrites)
			require.Equal(t, tt.expectedValidatedTx.validTxBlindWrites, validatedTxs.validTxBlindWrites)
			require.Equal(t, tt.expectedValidatedTx.invalidTxIndices, validatedTxs.invalidTxIndices)
		})
	}
}
