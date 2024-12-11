package vcservice

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type validatorAndCommitterServiceTestEnv struct {
	vcs          *ValidatorCommitterService
	client       protovcservice.ValidationAndCommitServiceClient
	stream       protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient
	streamCancel func()
	dbEnv        *DatabaseTestEnv
}

func newValidatorAndCommitServiceTestEnv(ctx context.Context, t *testing.T) *validatorAndCommitterServiceTestEnv {
	vcs := NewValidatorAndCommitServiceTestEnv(t)

	clientConn, err := connection.Connect(connection.NewDialConfig(vcs.Config.Server.Endpoint))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
	})
	client := protovcservice.NewValidationAndCommitServiceClient(clientConn)

	sCtx, sCancel := context.WithTimeout(ctx, 2*time.Minute)
	t.Cleanup(sCancel)
	vcStream, err := client.StartValidateAndCommitStream(sCtx)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return vcs.VCService.isStreamActive.Load()
	}, 2*time.Second, 50*time.Millisecond)
	t.Cleanup(func() {
		require.NoError(t, vcStream.CloseSend())
	})

	return &validatorAndCommitterServiceTestEnv{
		vcs:          vcs.VCService,
		client:       client,
		stream:       vcStream,
		streamCancel: sCancel,
		dbEnv:        vcs.DBEnv,
	}
}

func TestValidatorAndCommitterService(t *testing.T) {
	ctx := createContext(t)
	env := newValidatorAndCommitServiceTestEnv(ctx, t)

	env.dbEnv.populateDataWithCleanup(t, []int{1, int(types.MetaNamespaceID)}, namespaceToWrites{
		1: &namespaceWrites{
			keys:     [][]byte{[]byte("Existing key"), []byte("Existing key update")},
			values:   [][]byte{[]byte("value"), []byte("value")},
			versions: [][]byte{v0},
		},
		types.MetaNamespaceID: &namespaceWrites{
			keys:     [][]byte{types.NamespaceID(1).Bytes()},
			versions: [][]byte{v0},
		},
	}, nil, nil)

	t.Run("all valid txs", func(t *testing.T) {
		txBatch := &protovcservice.TransactionBatch{
			Transactions: []*protovcservice.Transaction{
				// The following 3 TXs test the blind write path, merging to the update path
				{
					ID: "Blind write without value",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: v0,
							BlindWrites: []*protoblocktx.Write{
								{
									Key: []byte("blind write without value"),
								},
							},
						},
					},
					BlockNumber: 1,
					TxNum:       1,
				},
				{
					ID: "Blind write with value",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: v0,
							BlindWrites: []*protoblocktx.Write{
								{
									Key:   []byte("Blind write with value"),
									Value: []byte("value2"),
								},
							},
						},
					},
					BlockNumber: 1,
					TxNum:       2,
				},
				{
					ID: "Blind write update existing key",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: v0,
							BlindWrites: []*protoblocktx.Write{
								{
									Key:   []byte("Existing key update"),
									Value: []byte("new-value"),
								},
							},
						},
					},
					BlockNumber: 2,
					TxNum:       3,
				},
				// The following 2 TXs test the new key path
				{
					ID: "New key with value",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: v0,
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key:   []byte("New key with value"),
									Value: []byte("value3"),
								},
							},
						},
					},
					BlockNumber: 2,
					TxNum:       4,
				},
				{
					ID: "New key no value",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: v0,
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key: []byte("New key no value"),
								},
							},
						},
					},
					BlockNumber: 3,
					TxNum:       5,
				},
				// The following TX tests the update path
				{
					ID: "Existing key",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: v0,
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key:     []byte("Existing key"),
									Value:   []byte("new-value"),
									Version: v0,
								},
							},
						},
					},
					BlockNumber: 2,
					TxNum:       6,
				},
			},
		}

		require.Zero(t, test.GetMetricValue(t, env.vcs.metrics.transactionReceivedTotal))

		require.NoError(t, env.stream.Send(txBatch))
		txStatus, err := env.stream.Recv()
		require.NoError(t, err)

		expectedTxStatus := &protovcservice.TransactionStatus{
			Status: map[string]protoblocktx.Status{},
		}
		expectedHeight := make(transactionIDToHeight)
		for _, tx := range txBatch.Transactions {
			expectedTxStatus.Status[tx.ID] = protoblocktx.Status_COMMITTED
			expectedHeight[TxID(tx.ID)] = types.NewHeight(tx.BlockNumber, tx.TxNum)
		}

		require.Equal(
			t,
			float64(len(txBatch.Transactions)),
			test.GetMetricValue(t, env.vcs.metrics.transactionReceivedTotal),
		)
		require.Equal(t, expectedTxStatus.Status, txStatus.Status)

		env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus.Status, expectedHeight)

		txBatch = &protovcservice.TransactionBatch{
			Transactions: []*protovcservice.Transaction{
				{
					ID: "New key 2 no value",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: v0,
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key: []byte("New key 2 no value"),
								},
							},
						},
					},
					BlockNumber: 2,
				},
			},
		}

		require.NoError(t, env.stream.Send(txBatch))

		require.Eventually(t, func() bool {
			txStatus, err = env.stream.Recv()
			require.NoError(t, err)
			require.Equal(t, protoblocktx.Status_COMMITTED, txStatus.Status["New key 2 no value"])
			return true
		}, env.vcs.timeoutForMinTxBatchSize, 500*time.Millisecond)
	})

	t.Run("invalid tx", func(t *testing.T) {
		txBatch := &protovcservice.TransactionBatch{
			Transactions: []*protovcservice.Transaction{
				{
					ID: "Namespace version mismatch",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: v1,
							BlindWrites: []*protoblocktx.Write{
								{
									Key: []byte("blind write without value"),
								},
							},
						},
					},
					BlockNumber: 4,
					TxNum:       1,
				},
				{
					ID: "prelim invalid tx",
					PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
						Code: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
					},
					BlockNumber: 5,
					TxNum:       2,
				},
			},
		}

		require.NoError(t, env.stream.Send(txBatch))
		txStatus, err := env.stream.Recv()
		require.NoError(t, err)

		expectedTxStatus := &protovcservice.TransactionStatus{
			Status: map[string]protoblocktx.Status{
				txBatch.Transactions[0].ID: protoblocktx.Status_ABORTED_MVCC_CONFLICT,
				txBatch.Transactions[1].ID: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
			},
		}
		expectedHeight := transactionIDToHeight{
			TxID(txBatch.Transactions[0].ID): types.NewHeight(
				txBatch.Transactions[0].BlockNumber,
				txBatch.Transactions[0].TxNum,
			),
			TxID(txBatch.Transactions[1].ID): types.NewHeight(
				txBatch.Transactions[1].BlockNumber,
				txBatch.Transactions[1].TxNum,
			),
		}
		require.Equal(t, expectedTxStatus.Status, txStatus.Status)

		env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus.Status, expectedHeight)

		status, err := env.client.GetTransactionsStatus(ctx, &protoblocktx.QueryStatus{
			TxIDs: []string{txBatch.Transactions[0].ID, txBatch.Transactions[1].ID},
		})
		require.NoError(t, err)
		require.Equal(t, expectedTxStatus.Status, status.Status)
	})
}

func TestWaitingTxsCount(t *testing.T) {
	ctx := createContext(t)
	env := newValidatorAndCommitServiceTestEnv(ctx, t)
	env.dbEnv.populateDataWithCleanup(t, nil, nil, nil, nil)
	// NOTE: We are setting the minTxBatchSize to 10 so that the
	//       received batch can wait for at most 5 seconds, which
	//       is the default timeout for minTxBatchSize. This should
	//       help to avoid flaky tests.
	//       This timer starts when the stream is started by the
	//       call to newValidatorAndCommitServiceTestEnv. By the
	//       time we send the batch, we can be sure that the
	//       batch would wait for the test to pass, as populateDataWithCleanup
	//       and other operations should not consume more than 10 seconds
	//       which is the timeoutForMinTxBatchSize defined in newValidatorAndCommitServiceTestEnv.
	env.vcs.minTxBatchSize = 10

	success := make(chan bool, 1)
	go func() {
		require.Eventually(t, func() bool {
			if env.vcs.numWaitingTxsForStatus.Load() == int32(1) {
				success <- true
				return true
			}
			return false
		}, 2*time.Second, 100*time.Microsecond)
		success <- false
	}()

	require.NoError(t, env.stream.Send(
		&protovcservice.TransactionBatch{
			Transactions: []*protovcservice.Transaction{
				{
					ID: "1",
					PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
						Code: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
					},
				},
			},
		},
	))
	require.True(t, <-success)

	count, err := env.client.NumberOfWaitingTransactionsForStatus(context.Background(), nil)
	require.Contains(t, err.Error(), "stream is still active")
	require.Nil(t, count)

	txStatus, err := env.stream.Recv()
	require.NoError(t, err)
	require.Len(t, txStatus.Status, 1)

	env.streamCancel()
	require.Eventually(t, func() bool {
		return !env.vcs.isStreamActive.Load()
	}, 2*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		wTxs, err := env.client.NumberOfWaitingTransactionsForStatus(context.Background(), nil)
		if err != nil {
			return false
		}
		return wTxs.GetCount() == 0
	}, 2*time.Second, 100*time.Millisecond)
}

func TestLastCommittedAndLastSeenBlockNumber(t *testing.T) {
	ctx := createContext(t)
	env := newValidatorAndCommitServiceTestEnv(ctx, t)
	lastCommittedBlock, err := env.client.GetLastCommittedBlockNumber(ctx, nil)
	require.Error(t, err, ErrMetadataEmpty)
	require.Nil(t, lastCommittedBlock)

	_, err = env.client.SetLastCommittedBlockNumber(ctx, &protoblocktx.BlockInfo{Number: 0})
	require.NoError(t, err)

	lastCommittedBlock, err = env.client.GetLastCommittedBlockNumber(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(0), lastCommittedBlock.Number)
}

func TestVCServiceOneActiveStreamOnly(t *testing.T) {
	ctx := createContext(t)
	env := newValidatorAndCommitServiceTestEnv(ctx, t)

	require.Eventually(t, func() bool {
		return env.vcs.isStreamActive.Load()
	}, 4*time.Second, 250*time.Millisecond)

	stream, err := env.client.StartValidateAndCommitStream(ctx)
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorContains(t, err, utils.ErrActiveStream.Error())
}

func TestTransactionResubmission(t *testing.T) {
	ctx := createContext(t)
	env := newValidatorAndCommitServiceTestEnv(ctx, t)

	env.dbEnv.populateDataWithCleanup(t, []int{3, int(types.MetaNamespaceID)}, namespaceToWrites{
		3: &namespaceWrites{
			keys:     [][]byte{[]byte("Existing key")},
			values:   [][]byte{[]byte("value")},
			versions: [][]byte{v0},
		},
		types.MetaNamespaceID: &namespaceWrites{
			keys:     [][]byte{types.NamespaceID(3).Bytes()},
			versions: [][]byte{v0},
		},
	}, nil, nil)

	txs := []struct {
		tx             *protovcservice.Transaction
		expectedStatus protoblocktx.Status
	}{
		{
			tx: &protovcservice.Transaction{
				ID: "Blind write with value",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      3,
						NsVersion: v0,
						BlindWrites: []*protoblocktx.Write{
							{
								Key:   []byte("Blind write with value"),
								Value: []byte("value2"),
							},
						},
					},
				},
				BlockNumber: 1,
				TxNum:       2,
			},
			expectedStatus: protoblocktx.Status_COMMITTED,
		},
		{
			tx: &protovcservice.Transaction{
				ID: "New key with value",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      3,
						NsVersion: v0,
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key:   []byte("New key with value"),
								Value: []byte("value3"),
							},
						},
					},
				},
				BlockNumber: 2,
				TxNum:       4,
			},
			expectedStatus: protoblocktx.Status_COMMITTED,
		},
		{
			tx: &protovcservice.Transaction{
				ID: "New key no value",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      3,
						NsVersion: v0,
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key: []byte("New key no value"),
							},
						},
					},
				},
				BlockNumber: 3,
				TxNum:       5,
			},
			expectedStatus: protoblocktx.Status_COMMITTED,
		},
		{
			tx: &protovcservice.Transaction{
				ID: "invalid sign",
				PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
					Code: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
				},
				BlockNumber: 3,
				TxNum:       6,
			},
			expectedStatus: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
		},
		{
			tx: &protovcservice.Transaction{
				ID: "duplicate namespace",
				PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
					Code: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
				},
				BlockNumber: 3,
				TxNum:       7,
			},
			expectedStatus: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
		},
		{
			tx: &protovcservice.Transaction{
				ID: "conflict",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      3,
						NsVersion: v0,
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key: []byte("Existing key"),
							},
						},
					},
				},
				BlockNumber: 3,
				TxNum:       8,
			},
			expectedStatus: protoblocktx.Status_ABORTED_MVCC_CONFLICT,
		},
	}

	txBatch := &protovcservice.TransactionBatch{}
	expectedTxStatus := &protovcservice.TransactionStatus{
		Status: map[string]protoblocktx.Status{},
	}
	expectedHeight := make(transactionIDToHeight)
	for _, t := range txs {
		txBatch.Transactions = append(txBatch.Transactions, t.tx)
		expectedTxStatus.Status[t.tx.ID] = t.expectedStatus
		expectedHeight[TxID(t.tx.ID)] = types.NewHeight(t.tx.BlockNumber, t.tx.TxNum)
	}

	// we are submitting the same three transactions thrice. We should consistently see the same status.
	for range 3 {
		require.NoError(t, env.stream.Send(txBatch))
		txStatus, err := env.stream.Recv()
		require.NoError(t, err)

		require.Equal(t, expectedTxStatus.Status, txStatus.Status)
		env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus.Status, expectedHeight)
	}
}

func createContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}
