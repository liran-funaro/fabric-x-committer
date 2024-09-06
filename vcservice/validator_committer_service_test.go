package vcservice

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

type validatorAndCommitterServiceTestEnv struct {
	vcs          *ValidatorCommitterService
	client       protovcservice.ValidationAndCommitServiceClient
	stream       protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient
	streamCancel func()
	dbEnv        *DatabaseTestEnv
	ctx          context.Context
}

func newValidatorAndCommitServiceTestEnv(t *testing.T) *validatorAndCommitterServiceTestEnv {
	dbEnv := NewDatabaseTestEnv(t)

	config := &ValidatorCommitterServiceConfig{
		Server: &connection.ServerConfig{
			Endpoint: connection.Endpoint{
				Host: "localhost",
				Port: 0,
			},
		},
		Database: dbEnv.DBConf,
		ResourceLimits: &ResourceLimitsConfig{
			MaxWorkersForPreparer:   2,
			MaxWorkersForValidator:  2,
			MaxWorkersForCommitter:  2,
			MinTransactionBatchSize: 1,
		},
		Monitoring: &monitoring.Config{
			Metrics: &metrics.Config{
				Enable: true,
				Endpoint: &connection.Endpoint{
					Host: "localhost",
					Port: 0,
				},
			},
		},
	}

	sConfig := connection.ServerConfig{
		Endpoint: connection.Endpoint{Host: "localhost", Port: 0},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	vcs, err := NewValidatorCommitterService(ctx, config)
	require.NoError(t, err)
	t.Cleanup(vcs.Close)

	var grpcSrv *grpc.Server

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		connection.RunServerMain(&sConfig, func(grpcServer *grpc.Server, actualListeningPort int) {
			grpcSrv = grpcServer
			sConfig.Endpoint.Port = actualListeningPort
			protovcservice.RegisterValidationAndCommitServiceServer(grpcServer, vcs)
			wg.Done()
		})
	}()

	wg.Wait()
	t.Cleanup(grpcSrv.Stop)

	clientConn, err := connection.Connect(connection.NewDialConfig(sConfig.Endpoint))
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
		return vcs.isStreamActive.Load()
	}, 2*time.Second, 50*time.Millisecond)
	t.Cleanup(func() {
		require.NoError(t, vcStream.CloseSend())
	})

	return &validatorAndCommitterServiceTestEnv{
		vcs:          vcs,
		client:       client,
		stream:       vcStream,
		streamCancel: sCancel,
		dbEnv:        dbEnv,
		ctx:          ctx,
	}
}

func TestValidatorAndCommitterService(t *testing.T) {
	env := newValidatorAndCommitServiceTestEnv(t)

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
	}, nil)

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
		for _, tx := range txBatch.Transactions {
			expectedTxStatus.Status[tx.ID] = protoblocktx.Status_COMMITTED
		}

		require.Equal(
			t,
			float64(len(txBatch.Transactions)),
			test.GetMetricValue(t, env.vcs.metrics.transactionReceivedTotal),
		)
		require.Equal(t, expectedTxStatus.Status, txStatus.Status)

		env.dbEnv.statusExists(t, expectedTxStatus.Status)

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
				},
				{
					ID: "prelim invalid tx",
					PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
						Code: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
					},
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
		require.Equal(t, expectedTxStatus.Status, txStatus.Status)

		env.dbEnv.statusExists(t, expectedTxStatus.Status)
	})
}

func TestWaitingTxsCount(t *testing.T) {
	env := newValidatorAndCommitServiceTestEnv(t)
	env.dbEnv.populateDataWithCleanup(t, nil, nil, nil)
	// NOTE: We are setting the minTxBatchSize to 10 so that the
	//       received batch can wait for at most 5 seconds, which
	//       is the default timeout for minTxBatchSize. This should
	//       help to avoid flaky tests.
	//       This timer starts when the stream is started by the
	//       call to newValidatorAndCommitServiceTestEnv. By the
	//       time we send the batch, we can be sure that the
	//       batch would wait for the test to pass, as populateDataWithCleanup
	//       and other operations should not consume more than 5 seconds.
	//       Once we make the timeoutForMinTxBatchSize configurable, we can
	//       increase the timeout further.
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

func TestLastCommittedBlockNumber(t *testing.T) {
	env := newValidatorAndCommitServiceTestEnv(t)
	require.NoError(t, initDatabaseTables(context.Background(), env.dbEnv.DB.pool, nil))
	b, err := env.client.GetLastCommittedBlockNumber(env.ctx, nil)
	require.Error(t, err, ErrNoBlockCommitted)
	require.Nil(t, b)

	_, err = env.client.SetLastCommittedBlockNumber(env.ctx, &protoblocktx.LastCommittedBlock{Number: 0})
	require.NoError(t, err)

	b, err = env.client.GetLastCommittedBlockNumber(env.ctx, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(0), b.Number)
}
