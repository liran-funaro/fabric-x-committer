package vcservice

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

type validatorAndCommitterServiceTestEnv struct {
	vcs        *ValidatorCommitterService
	grpcServer *grpc.Server
	clientConn *grpc.ClientConn
}

func newValidatorAndCommitServiceTestEnv(t *testing.T) *validatorAndCommitterServiceTestEnv {
	db := &runner.YugabyteDB{}
	require.NoError(t, db.Start())

	dbConnSettings := db.ConnectionSettings()
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
			Host:     dbConnSettings.Host,
			Port:     port,
			Username: dbConnSettings.User,
			Password: dbConnSettings.Password,
		},
		ResourceLimits: &ResourceLimitsConfig{
			MaxWorkersForPreparer:  2,
			MaxWorkersForValidator: 2,
			MaxWorkersForCommitter: 2,
		},
	}

	sConfig := connection.ServerConfig{
		Endpoint: connection.Endpoint{Host: "localhost", Port: 0},
	}

	vcs := NewValidatorCommitterService(config)

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
		stopDB(t, db)
	})

	return &validatorAndCommitterServiceTestEnv{
		vcs:        vcs,
		grpcServer: grpcSrv,
		clientConn: clientConn,
	}
}

func TestValidatorAndCommitterService(t *testing.T) {
	env := newValidatorAndCommitServiceTestEnv(t)

	populateDataWithCleanup(t, env.vcs.databaseConnection, []namespaceID{1, txIDsStatusNameSpace}, namespaceToWrites{})

	client := protovcservice.NewValidationAndCommitServiceClient(env.clientConn)

	txBatch := &protovcservice.TransactionBatch{
		Transactions: []*protovcservice.Transaction{
			{
				ID: "tx1",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: 1,
						BlindWrites: []*protoblocktx.Write{
							{
								Key:   "key1",
								Value: []byte("value1"),
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

	require.NoError(t, vcStream.Send(txBatch))
	txStatus, err := vcStream.Recv()
	require.NoError(t, err)

	expectedTxStatus := &protovcservice.TransactionStatus{
		Status: map[string]protovcservice.TransactionStatus_Flag{
			"tx1": protovcservice.TransactionStatus_COMMITTED,
		},
	}

	require.Equal(t, expectedTxStatus.Status, txStatus.Status)
}
