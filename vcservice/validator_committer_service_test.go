package vcservice

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	vcs        *ValidatorCommitterService
	grpcServer *grpc.Server
	clientConn *grpc.ClientConn
	dbEnv      *DatabaseTestEnv
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
			MinTransactionBatchSize: 10,
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

	vcs, err := NewValidatorCommitterService(config)
	require.NoError(t, err)

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

	clientConn, err := connection.Connect(connection.NewDialConfig(sConfig.Endpoint))
	require.NoError(t, err)

	t.Cleanup(func() {
		assert.NoError(t, clientConn.Close())
		grpcSrv.Stop()
		vcs.Close()
	})

	return &validatorAndCommitterServiceTestEnv{
		vcs:        vcs,
		grpcServer: grpcSrv,
		clientConn: clientConn,
		dbEnv:      dbEnv,
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

	client := protovcservice.NewValidationAndCommitServiceClient(env.clientConn)

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

		vcStream, err := client.StartValidateAndCommitStream(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, vcStream.CloseSend())
		})

		require.Zero(t, test.GetMetricValue(t, env.vcs.metrics.transactionReceivedTotal))

		require.NoError(t, vcStream.Send(txBatch))
		txStatus, err := vcStream.Recv()
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

		env.vcs.minTxBatchSize = 1
		require.NoError(t, vcStream.Send(txBatch))

		require.Eventually(t, func() bool {
			txStatus, err = vcStream.Recv()
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

		vcStream, err := client.StartValidateAndCommitStream(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, vcStream.CloseSend())
		})

		require.NoError(t, vcStream.Send(txBatch))
		txStatus, err := vcStream.Recv()
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
