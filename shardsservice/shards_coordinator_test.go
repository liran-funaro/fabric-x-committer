package shardsservice

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/pendingcommits"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/grpc"
)

type shardsCoordinatorGrpcServiceForTest struct {
	sc         *shardsCoordinator
	grpcServer *grpc.Server
	clientConn *grpc.ClientConn
	cleanup    func()
}

func NewShardsCoordinatorGrpcServiceForTest(t *testing.T, port int) *shardsCoordinatorGrpcServiceForTest {
	c := ShardServiceConfig{
		Prometheus: monitoring.Prometheus{Enabled: false},
		Endpoint: connection.Endpoint{
			Host: "localhost",
			Port: port,
		},
		Database: &DatabaseConfig{
			Type:    "rocksdb",
			RootDir: "./",
		},
		Limits: &LimitsConfig{
			MaxPhaseOneResponseBatchItemCount: 100,
			PhaseOneResponseCutTimeout:        10 * time.Millisecond,
			MaxPhaseOneProcessingWorkers:      50,
			MaxPhaseTwoProcessingWorkers:      50,
			MaxPendingCommitsBufferSize:       100,
			MaxShardInstancesBufferSize:       100,
		},
	}

	var grpcServer *grpc.Server
	sc := NewShardsCoordinator(c.Database, c.Limits, metrics.New(false))
	go connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(server *grpc.Server) {
		log.Print("created shards coordinator")
		grpcServer = server
		RegisterShardsServer(server, sc)
	})

	clientConn, err := connection.Connect(connection.NewDialConfig(c.Endpoint))
	require.NoError(t, err)

	return &shardsCoordinatorGrpcServiceForTest{
		sc:         sc,
		grpcServer: grpcServer,
		clientConn: clientConn,
		cleanup: func() {
			sc.shards.deleteAll()
			clientConn.Close()
			grpcServer.GracefulStop()
		},
	}
}

func TestShardsCoordinator(t *testing.T) {
	shardService := NewShardsCoordinatorGrpcServiceForTest(t, 6002)
	defer shardService.cleanup()

	client := NewShardsClient(shardService.clientConn)
	_, err := client.DeleteShards(context.Background(), &Empty{})
	require.NoError(t, err)

	_, err = client.SetupShards(context.Background(), &ShardsSetupRequest{FirstShardId: 1, LastShardId: 4})
	require.NoError(t, err)

	phaseOneStream, err := client.StartPhaseOneStream(context.Background())
	require.NoError(t, err)
	defer phaseOneStream.CloseSend()

	phase1Requests := &PhaseOneRequestBatch{
		Requests: []*PhaseOneRequest{
			{
				BlockNum: 1,
				TxNum:    1,
				ShardidToSerialNumbers: map[uint32]*SerialNumbers{
					1: {
						SerialNumbers: [][]byte{[]byte("key1"), []byte("key2")},
					},
					3: {
						SerialNumbers: [][]byte{[]byte("key3"), []byte("key4")},
					},
				},
			},
			{
				BlockNum: 1,
				TxNum:    2,
				ShardidToSerialNumbers: map[uint32]*SerialNumbers{
					1: {
						SerialNumbers: [][]byte{[]byte("key5"), []byte("key6")},
					},
					2: {
						SerialNumbers: [][]byte{[]byte("key7"), []byte("key8")},
					},
				},
			},
		},
	}

	require.NoError(t, phaseOneStream.Send(phase1Requests))
	phase1Responses, err := phaseOneStream.Recv()
	require.NoError(t, err)

	expectedPhase1Responses := map[pendingcommits.TxID]PhaseOneResponse_Status{
		{
			BlkNum: 1,
			TxNum:  1,
		}: PhaseOneResponse_CAN_COMMIT,
		{
			BlkNum: 1,
			TxNum:  2,
		}: PhaseOneResponse_CAN_COMMIT,
	}

	require.Equal(t, len(expectedPhase1Responses), len(phase1Responses.Responses))
	for _, resp := range phase1Responses.Responses {
		expectedStatus := expectedPhase1Responses[pendingcommits.TxID{BlkNum: resp.BlockNum, TxNum: resp.TxNum}]
		require.Equal(t, expectedStatus, resp.Status)
	}

}
