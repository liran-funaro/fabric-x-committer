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
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type validatorAndCommitterServiceTestEnv struct {
	vcs     []*ValidatorCommitterService
	clients []protovcservice.ValidationAndCommitServiceClient
	streams []protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient
	dbEnv   *DatabaseTestEnv
}

func newValidatorAndCommitServiceTestEnv(
	ctx context.Context,
	t *testing.T,
	numServices int,
) *validatorAndCommitterServiceTestEnv {
	vcs := NewValidatorAndCommitServiceTestEnv(t, numServices)

	vcsTestEnv := &validatorAndCommitterServiceTestEnv{
		vcs:     vcs.VCServices,
		clients: make([]protovcservice.ValidationAndCommitServiceClient, numServices),
		streams: make([]protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient, numServices),
		dbEnv:   vcs.DBEnv,
	}

	for i := range numServices {
		clientConn, err := connection.Connect(connection.NewDialConfig(&vcs.Configs[i].Server.Endpoint))
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
			return vcs.VCServices[i].isStreamActive.Load()
		}, 2*time.Second, 50*time.Millisecond)

		vcsTestEnv.clients[i] = client
		vcsTestEnv.streams[i] = vcStream
	}

	return vcsTestEnv
}

func TestValidatorAndCommitterService(t *testing.T) {
	ctx, _ := createContext(t)
	env := newValidatorAndCommitServiceTestEnv(ctx, t, 1)

	env.dbEnv.populateDataWithCleanup(t, []string{"1", types.MetaNamespaceID}, namespaceToWrites{
		"1": &namespaceWrites{
			keys:     [][]byte{[]byte("Existing key"), []byte("Existing key update")},
			values:   [][]byte{[]byte("value"), []byte("value")},
			versions: [][]byte{v0},
		},
		types.MetaNamespaceID: &namespaceWrites{
			keys:     [][]byte{[]byte("1")},
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
							NsId:      "1",
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
							NsId:      "1",
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
							NsId:      "1",
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
							NsId:      "1",
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
							NsId:      "1",
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
							NsId:      "1",
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

		require.Zero(t, test.GetMetricValue(t, env.vcs[0].metrics.transactionReceivedTotal))

		require.NoError(t, env.streams[0].Send(txBatch))
		txStatus, err := env.streams[0].Recv()
		require.NoError(t, err)

		expectedTxStatus := make(map[string]*protoblocktx.StatusWithHeight)
		txIDs := make([]string, len(txBatch.Transactions))
		for i, tx := range txBatch.Transactions {
			expectedTxStatus[tx.ID] = types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, tx.BlockNumber,
				int(tx.TxNum))
			txIDs[i] = tx.ID
		}

		require.Equal(
			t,
			float64(len(txBatch.Transactions)),
			test.GetMetricValue(t, env.vcs[0].metrics.transactionReceivedTotal),
		)
		require.Equal(t, expectedTxStatus, txStatus.Status)

		env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus)

		test.EnsurePersistedTxStatus(ctx, t, env.clients[0], txIDs, expectedTxStatus)

		txBatch = &protovcservice.TransactionBatch{
			Transactions: []*protovcservice.Transaction{
				{
					ID: "New key 2 no value",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      "1",
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

		require.NoError(t, env.streams[0].Send(txBatch))

		require.Eventually(t, func() bool {
			txStatus, err = env.streams[0].Recv()
			require.NoError(t, err)
			require.Equal(t, protoblocktx.Status_COMMITTED, txStatus.Status["New key 2 no value"].Code)
			return true
		}, env.vcs[0].timeoutForMinTxBatchSize, 500*time.Millisecond)
	})

	t.Run("invalid tx", func(t *testing.T) {
		txBatch := &protovcservice.TransactionBatch{
			Transactions: []*protovcservice.Transaction{
				{
					ID: "Namespace version mismatch",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      "1",
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
				{
					ID: "invalid new writes",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      "1",
							NsVersion: v0,
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key:     []byte("Existing key"),
									Value:   []byte("new-value"),
									Version: nil,
								},
							},
						},
					},
					BlockNumber: 2,
					TxNum:       6,
				},
			},
		}

		require.NoError(t, env.streams[0].Send(txBatch))
		txStatus, err := env.streams[0].Recv()
		require.NoError(t, err)

		expectedStatus := []protoblocktx.Status{
			protoblocktx.Status_ABORTED_MVCC_CONFLICT,
			protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
			protoblocktx.Status_ABORTED_MVCC_CONFLICT,
		}

		expectedTxStatus := make(map[string]*protoblocktx.StatusWithHeight, len(txBatch.Transactions))
		txIDs := make([]string, len(txBatch.Transactions))
		for i, tx := range txBatch.Transactions {
			expectedTxStatus[tx.ID] = types.CreateStatusWithHeight(expectedStatus[i], tx.BlockNumber, int(tx.TxNum))
			txIDs = append(txIDs, tx.ID)
		}

		require.Equal(t, expectedTxStatus, txStatus.Status)

		env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus)

		status, err := env.clients[0].GetTransactionsStatus(ctx, &protoblocktx.QueryStatus{TxIDs: txIDs})
		require.NoError(t, err)
		require.Equal(t, expectedTxStatus, status.Status)
	})
}

func TestWaitingTxsCount(t *testing.T) {
	ctx, cancel := createContext(t)
	env := newValidatorAndCommitServiceTestEnv(ctx, t, 1)
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
	env.vcs[0].minTxBatchSize = 10

	success := make(chan bool, 1)
	go func() {
		require.Eventually(t, func() bool {
			if env.vcs[0].numWaitingTxsForStatus.Load() == int32(1) {
				success <- true
				return true
			}
			return false
		}, 2*time.Second, 100*time.Microsecond)
		success <- false
	}()

	require.NoError(t, env.streams[0].Send(
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

	count, err := env.clients[0].NumberOfWaitingTransactionsForStatus(context.Background(), nil)
	require.Contains(t, err.Error(), "stream is still active")
	require.Nil(t, count)

	txStatus, err := env.streams[0].Recv()
	require.NoError(t, err)
	require.Len(t, txStatus.Status, 1)

	cancel()
	require.Eventually(t, func() bool {
		return !env.vcs[0].isStreamActive.Load()
	}, 2*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		wTxs, err := env.clients[0].NumberOfWaitingTransactionsForStatus(context.Background(), nil)
		if err != nil {
			return false
		}
		return wTxs.GetCount() == 0
	}, 2*time.Second, 100*time.Millisecond)
}

func TestLastCommittedAndLastSeenBlockNumber(t *testing.T) {
	ctx, _ := createContext(t)
	numServices := 3
	env := newValidatorAndCommitServiceTestEnv(ctx, t, numServices)

	for i := range numServices {
		lastCommittedBlock, err := env.clients[i].GetLastCommittedBlockNumber(ctx, nil)
		require.Error(t, err, ErrMetadataEmpty)
		require.Nil(t, lastCommittedBlock)
	}

	_, err := env.clients[0].SetLastCommittedBlockNumber(ctx, &protoblocktx.BlockInfo{Number: 0})
	require.NoError(t, err)

	for i := range numServices {
		lastCommittedBlock, err := env.clients[i].GetLastCommittedBlockNumber(ctx, nil)
		require.NoError(t, err)
		require.Equal(t, uint64(0), lastCommittedBlock.Number)
	}
}

func TestVCServiceOneActiveStreamOnly(t *testing.T) {
	ctx, _ := createContext(t)
	env := newValidatorAndCommitServiceTestEnv(ctx, t, 1)

	require.Eventually(t, func() bool {
		return env.vcs[0].isStreamActive.Load()
	}, 4*time.Second, 250*time.Millisecond)

	stream, err := env.clients[0].StartValidateAndCommitStream(ctx)
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorContains(t, err, utils.ErrActiveStream.Error())
}

func TestTransactionResubmission(t *testing.T) {
	setup := func() (context.Context, *validatorAndCommitterServiceTestEnv) {
		ctx, _ := createContext(t)
		numServices := 3
		env := newValidatorAndCommitServiceTestEnv(ctx, t, numServices)

		env.dbEnv.populateDataWithCleanup(t, []string{"3", types.MetaNamespaceID}, namespaceToWrites{
			"3": &namespaceWrites{
				keys:     [][]byte{[]byte("Existing key")},
				values:   [][]byte{[]byte("value")},
				versions: [][]byte{v0},
			},
			types.MetaNamespaceID: &namespaceWrites{
				keys:     [][]byte{[]byte("3")},
				versions: [][]byte{v0},
			},
		}, nil, nil)

		return ctx, env
	}

	txs := []struct {
		tx             *protovcservice.Transaction
		expectedStatus protoblocktx.Status
	}{
		{
			tx: &protovcservice.Transaction{
				ID: "Blind write with value",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      "3",
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
						NsId:      "3",
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
						NsId:      "3",
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
						NsId:      "3",
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
	expectedTxStatus := make(map[string]*protoblocktx.StatusWithHeight)
	txIDs := make([]string, len(txs))
	for i, t := range txs {
		txBatch.Transactions = append(txBatch.Transactions, t.tx)
		expectedTxStatus[t.tx.ID] = types.CreateStatusWithHeight(t.expectedStatus, t.tx.BlockNumber,
			int(t.tx.TxNum))
		txIDs[i] = t.tx.ID
	}

	t.Run("same transactions submitted again after commit", func(t *testing.T) {
		ctx, env := setup()
		require.NoError(t, env.streams[0].Send(txBatch))
		txStatus, err := env.streams[0].Recv()
		require.NoError(t, err)

		require.Equal(t, expectedTxStatus, txStatus.Status)
		env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus)

		// we are submitting the same three transactions again to all vcservices.
		// We should consistently see the same status.
		for i := range 3 {
			require.NoError(t, env.streams[i].Send(txBatch))
			txStatus, err := env.streams[i].Recv()
			require.NoError(t, err)

			require.Equal(t, expectedTxStatus, txStatus.Status)
			env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus)
		}

		test.EnsurePersistedTxStatus(ctx, t, env.clients[0], txIDs, expectedTxStatus)
	})

	t.Run("same transactions submitted again while previous submission is not yet committed", func(t *testing.T) {
		ctx, env := setup()
		require.NoError(t, env.streams[0].Send(txBatch))
		require.NoError(t, env.streams[0].Send(txBatch))
		for range 2 {
			// as minbatchsize used for test is 1, we should receive two status batches.
			txStatus, err := env.streams[0].Recv()
			require.NoError(t, err)

			require.Equal(t, expectedTxStatus, txStatus.Status)
		}
		env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus)
		test.EnsurePersistedTxStatus(ctx, t, env.clients[0], txIDs, expectedTxStatus)
	})

	t.Run("same transactions submitted again within the minbatchsize", func(t *testing.T) {
		ctx, env := setup()
		logging.SetupWithConfig(&logging.Config{
			Enabled: true,
			Level:   logging.Debug,
		})
		txBatchWithDup := &protovcservice.TransactionBatch{}
		txBatchWithDup.Transactions = append(txBatchWithDup.Transactions, txBatch.Transactions...)
		txBatchWithDup.Transactions = append(txBatchWithDup.Transactions, txBatch.Transactions...)
		require.NoError(t, env.streams[0].Send(txBatchWithDup))

		txStatus, err := env.streams[0].Recv()
		require.NoError(t, err)
		require.Equal(t, expectedTxStatus, txStatus.Status)

		env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus)
		test.EnsurePersistedTxStatus(ctx, t, env.clients[0], txIDs, expectedTxStatus)
	})

	t.Run("same duplicated transactions submitted in parallel to all vcservices", func(t *testing.T) {
		ctx, env := setup()
		txBatchWithDup := &protovcservice.TransactionBatch{}
		for range 10 {
			txBatchWithDup.Transactions = append(txBatchWithDup.Transactions, txBatch.Transactions...)
			txBatchWithDup.Transactions = append(txBatchWithDup.Transactions, txBatch.Transactions...)
		}

		for i := range 3 {
			require.NoError(t, env.streams[i].Send(txBatch))
		}

		for i := range 3 {
			txStatus, err := env.streams[i].Recv()
			require.NoError(t, err)
			require.Equal(t, expectedTxStatus, txStatus.Status)
		}
		env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus)
		test.EnsurePersistedTxStatus(ctx, t, env.clients[0], txIDs, expectedTxStatus)
	})
}

func createContext(t *testing.T) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx, cancel
}
