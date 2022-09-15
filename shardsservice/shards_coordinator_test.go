package shardsservice

import (
	"context"
	"fmt"
	"log"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type shardsCoordinatorGrpcServiceForTest struct {
	sc         *shardsCoordinator
	conf       *Configuration
	grpcServer *grpc.Server
	clientConn *grpc.ClientConn
	cleanup    func()
}

func newShardsCoordinatorGrpcServiceForTest(t *testing.T, port uint32) *shardsCoordinatorGrpcServiceForTest {
	conf := &Configuration{
		Database: &DatabaseConf{
			Name:    "rocksdb",
			RootDir: "./",
		},
		Limits: &LimitsConf{
			MaxGoroutines:                     100,
			MaxPhaseOneResponseBatchItemCount: 100,
			PhaseOneResponseCutTimeout:        50 * time.Millisecond,
		},
	}
	sc := NewShardsCoordinator(conf)
	log.Print("created shards coordinator")

	serverAddr := fmt.Sprintf("localhost:%d", port)
	lis, err := net.Listen("tcp", serverAddr)
	require.NoError(t, err)
	log.Print("listining on " + serverAddr)

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	RegisterShardsServer(grpcServer, sc)
	go func() {
		require.NoError(t, grpcServer.Serve(lis))
	}()

	cliOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	clientConn, err := grpc.Dial(serverAddr, cliOpts...)
	require.NoError(t, err)

	return &shardsCoordinatorGrpcServiceForTest{
		sc:         sc,
		conf:       conf,
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
	shardService := newShardsCoordinatorGrpcServiceForTest(t, 6002)
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

	expectedPhase1Responses := map[txID]PhaseOneResponse_Status{
		{
			blockNum: 1,
			txNum:    1,
		}: PhaseOneResponse_CAN_COMMIT,
		{
			blockNum: 1,
			txNum:    2,
		}: PhaseOneResponse_CAN_COMMIT,
	}

	require.Equal(t, len(expectedPhase1Responses), len(phase1Responses.Responses))
	for _, resp := range phase1Responses.Responses {
		expectedStatus := expectedPhase1Responses[txID{blockNum: resp.BlockNum, txNum: resp.TxNum}]
		require.Equal(t, expectedStatus, resp.Status)
	}

}
