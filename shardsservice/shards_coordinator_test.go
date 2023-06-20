package shardsservice

import (
	"context"
	"log"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/protos/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/pendingcommits"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
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
		Monitoring: monitoring.Config{},
		Server: &connection.ServerConfig{Endpoint: connection.Endpoint{
			Host: "localhost",
			Port: port,
		}},
		Database: &DatabaseConfig{
			Type:    "goleveldb",
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
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	sc := NewShardsCoordinator(c.Database, c.Limits, m)

	listener, err := net.Listen("tcp", c.Server.Endpoint.Address())
	require.NoError(t, err)
	if c.Server.Endpoint.Port == 0 {
		c.Server.Endpoint.Port = listener.Addr().(*net.TCPAddr).Port
	}

	grpcServer = grpc.NewServer(c.Server.Opts()...)
	shardsservice.RegisterShardsServer(grpcServer, sc)

	go func() {
		err = grpcServer.Serve(listener)
		if err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	clientConn, err := connection.Connect(connection.NewDialConfig(c.Server.Endpoint))
	require.NoError(t, err)

	return &shardsCoordinatorGrpcServiceForTest{
		sc:         sc,
		grpcServer: grpcServer,
		clientConn: clientConn,
		cleanup: func() {
			sc.shards.deleteAll()
			require.NoError(t, clientConn.Close())
			grpcServer.Stop()
		},
	}
}

func TestShardsCoordinator(t *testing.T) {
	shardService := NewShardsCoordinatorGrpcServiceForTest(t, 0)
	defer shardService.cleanup()

	client := shardsservice.NewShardsClient(shardService.clientConn)
	_, err := client.DeleteShards(context.Background(), &shardsservice.Empty{})
	require.NoError(t, err)

	_, err = client.SetupShards(context.Background(), &shardsservice.ShardsSetupRequest{FirstShardId: 1, LastShardId: 4})
	require.NoError(t, err)

	phaseOneStream, err := client.StartPhaseOneStream(context.Background())
	require.NoError(t, err)
	defer phaseOneStream.CloseSend()

	phase1Requests := &shardsservice.PhaseOneRequestBatch{
		Requests: []*shardsservice.PhaseOneRequest{
			{
				BlockNum: 1,
				TxNum:    1,
				ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
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
				ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
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

	expectedPhase1Responses := map[pendingcommits.TxID]shardsservice.PhaseOneResponse_Status{
		{
			BlkNum: 1,
			TxNum:  1,
		}: shardsservice.PhaseOneResponse_CAN_COMMIT,
		{
			BlkNum: 1,
			TxNum:  2,
		}: shardsservice.PhaseOneResponse_CAN_COMMIT,
	}

	require.Equal(t, len(expectedPhase1Responses), len(phase1Responses.Responses))
	for _, resp := range phase1Responses.Responses {
		expectedStatus := expectedPhase1Responses[pendingcommits.TxID{BlkNum: resp.BlockNum, TxNum: resp.TxNum}]
		require.Equal(t, expectedStatus, resp.Status)
	}

}
