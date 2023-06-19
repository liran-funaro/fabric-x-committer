package shardsservice

import (
	"context"
	"sync"
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

	var grpcSrv *grpc.Server
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	sc := NewShardsCoordinator(c.Database, c.Limits, m)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		connection.RunServerMain(c.Server, func(grpcServer *grpc.Server, actualListeningPort int) {
			grpcSrv = grpcServer
			port = actualListeningPort
			shardsservice.RegisterShardsServer(grpcServer, sc)
			wg.Done()
		})
	}()

	wg.Wait()

	if c.Server.Endpoint.Port == 0 {
		c.Server.Endpoint.Port = port
	}

	clientConn, err := connection.Connect(connection.NewDialConfig(c.Server.Endpoint))
	require.NoError(t, err)

	return &shardsCoordinatorGrpcServiceForTest{
		sc:         sc,
		grpcServer: grpcSrv,
		clientConn: clientConn,
		cleanup: func() {
			sc.shards.deleteAll()
			require.NoError(t, clientConn.Close())
			grpcSrv.Stop()
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
