package vcservice

import (
	"testing"

	"github.com/stretchr/testify/require"
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
	v := newValidator(dbEnv.db, preparedTxs, validatedTxs)

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

	env.dbEnv.populateDataWithCleanup(
		t,
		[]namespaceID{1, 2},
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
		},
	)

	tx1NonBlindWrites := namespaceToWrites{
		1: {
			keys:     []string{"key1.1"},
			values:   [][]byte{[]byte("value1.1.1")},
			versions: [][]byte{v2},
		},
		2: {
			keys:     []string{"key2.1"},
			values:   [][]byte{[]byte("value1.1.1")},
			versions: [][]byte{v2},
		},
	}
	tx2NonBlindWrites := namespaceToWrites{
		1: {
			keys:     []string{"key1.5"},
			values:   [][]byte{[]byte("value1.5.1")},
			versions: [][]byte{v0},
		},
	}
	tx3NonBlindWrites := namespaceToWrites{
		2: {
			keys:     []string{"key2.2"},
			values:   [][]byte{[]byte("value2.2.1")},
			versions: [][]byte{v2},
		},
	}
	tx3BlindWrites := namespaceToWrites{
		1: {
			keys:     []string{"key1.6"},
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
						keys:     []string{"key1.1", "key1.2", "key1.5"},
						versions: [][]byte{v1, v1, nil},
					},
					2: &reads{
						keys:     []string{"key2.1", "key2.2", "key2.5"},
						versions: [][]byte{v0, v0, nil},
					},
				},
				readToTransactionIndices: readToTransactions{
					comparableRead{1, "key1.1", string(v1)}: []TxID{"tx1"},
					comparableRead{1, "key1.2", string(v1)}: []TxID{"tx1"},
					comparableRead{1, "key1.5", ""}:         []TxID{"tx2"},
					comparableRead{2, "key2.1", string(v0)}: []TxID{"tx1"},
					comparableRead{2, "key2.2", string(v0)}: []TxID{"tx3"},
					comparableRead{2, "key2.5", ""}:         []TxID{"tx3"},
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
				invalidTxIndices: map[TxID]bool{},
			},
		},
		{
			name: "all invalid tx",
			preparedTx: &preparedTransactions{
				namespaceToReadEntries: namespaceToReads{
					1: &reads{
						keys:     []string{"key1.1", "key1.2", "key1.5"},
						versions: [][]byte{v0, v0, v1},
					},
					2: &reads{
						keys:     []string{"key2.1", "key2.2", "key2.5"},
						versions: [][]byte{nil, nil, nil},
					},
				},
				readToTransactionIndices: readToTransactions{
					comparableRead{1, "key1.1", string(v0)}: []TxID{"tx1"},
					comparableRead{1, "key1.2", string(v0)}: []TxID{"tx1"},
					comparableRead{1, "key1.5", string(v1)}: []TxID{"tx2"},
					comparableRead{2, "key2.1", ""}:         []TxID{"tx1"},
					comparableRead{2, "key2.2", ""}:         []TxID{"tx3"},
					comparableRead{2, "key2.5", ""}:         []TxID{"tx3"},
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
				invalidTxIndices: map[TxID]bool{
					"tx1": true,
					"tx2": true,
					"tx3": true,
				},
			},
		},
		{
			name: "valid and invalid tx",
			preparedTx: &preparedTransactions{
				namespaceToReadEntries: namespaceToReads{
					1: &reads{
						keys:     []string{"key1.1", "key1.2", "key1.5"},
						versions: [][]byte{v1, v1, nil},
					},
					2: &reads{
						keys:     []string{"key2.1", "key2.2", "key2.5"},
						versions: [][]byte{nil, nil, nil},
					},
				},
				readToTransactionIndices: readToTransactions{
					comparableRead{1, "key1.1", string(v1)}: []TxID{"tx1"},
					comparableRead{1, "key1.2", string(v1)}: []TxID{"tx1"},
					comparableRead{1, "key1.5", ""}:         []TxID{"tx2"},
					comparableRead{2, "key2.1", ""}:         []TxID{"tx1"},
					comparableRead{2, "key2.2", ""}:         []TxID{"tx3"},
					comparableRead{2, "key2.5", ""}:         []TxID{"tx3"},
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
				invalidTxIndices: map[TxID]bool{
					"tx1": true,
					"tx3": true,
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
