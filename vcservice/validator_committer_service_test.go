package vcservice

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	promgo "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"google.golang.org/grpc"
)

type validatorAndCommitterServiceTestEnv struct {
	vcs        *ValidatorCommitterService
	grpcServer *grpc.Server
	clientConn *grpc.ClientConn
	dbEnv      *databaseTestEnv
}

func newValidatorAndCommitServiceTestEnv(t *testing.T) *validatorAndCommitterServiceTestEnv {
	dbEnv := newDatabaseTestEnv(t)

	dbConnSettings := dbEnv.dbRunner.ConnectionSettings()
	port, err := strconv.Atoi(dbConnSettings.Port)
	require.NoError(t, err)

	config := &ValidatorCommitterServiceConfig{
		Server: &connection.ServerConfig{
			Endpoint: connection.Endpoint{
				Host: "localhost",
				Port: 0,
			},
		},
		Database: &DatabaseConfig{
			Host:           dbConnSettings.Host,
			Port:           port,
			Username:       dbConnSettings.User,
			Password:       dbConnSettings.Password,
			MaxConnections: 20,
			MinConnections: 10,
		},
		ResourceLimits: &ResourceLimitsConfig{
			MaxWorkersForPreparer:  2,
			MaxWorkersForValidator: 2,
			MaxWorkersForCommitter: 2,
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
		vcs.close()
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

	env.dbEnv.populateDataWithCleanup(t, []int{1}, namespaceToWrites{
		1: &namespaceWrites{
			keys:     [][]byte{[]byte("Existing key"), []byte("Existing key update")},
			values:   [][]byte{[]byte("value"), []byte("value")},
			versions: [][]byte{v0},
		},
	}, nil)

	client := protovcservice.NewValidationAndCommitServiceClient(env.clientConn)

	txBatch := &protovcservice.TransactionBatch{
		Transactions: []*protovcservice.Transaction{
			// The following 3 TXs test the blind write path, merging to the update path
			{
				ID: "Blind write without value",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
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
						NsId: 1,
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
						NsId: 1,
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
						NsId: 1,
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
						NsId: 1,
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
						NsId: 1,
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

	require.Zero(t, getMetricsValue(t, env.vcs.metrics.transactionReceivedTotal))

	require.NoError(t, vcStream.Send(txBatch))
	txStatus, err := vcStream.Recv()
	require.NoError(t, err)

	expectedTxStatus := &protovcservice.TransactionStatus{
		Status: map[string]protoblocktx.Status{},
	}
	for _, tx := range txBatch.Transactions {
		expectedTxStatus.Status[tx.ID] = protoblocktx.Status_COMMITTED
	}

	require.Equal(t, float64(len(txBatch.Transactions)), getMetricsValue(t, env.vcs.metrics.transactionReceivedTotal))
	require.Equal(t, expectedTxStatus.Status, txStatus.Status)

	env.dbEnv.statusExists(t, expectedTxStatus.Status)
}

func getMetricsValue(t *testing.T, m prometheus.Metric) float64 {
	gm := promgo.Metric{}
	require.NoError(t, m.Write(&gm))
	return gm.Counter.GetValue()
}
