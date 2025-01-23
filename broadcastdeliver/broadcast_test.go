package broadcastdeliver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
)

func init() {
	logging.NoGrpcLog()
}

func TestServers(t *testing.T) {
	mocks, orderers, conf := makeConfig(t)

	client, err := New(&conf)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	stream, err := client.Broadcast(ctx)
	require.NoError(t, err)

	// All good.
	recv, err := sendRecv(t, stream)
	require.NoError(t, err)
	require.Equal(t, 3, strings.Count(recv, "SUCCESS"))
	require.Equal(t, 0, strings.Count(recv, "Unavailable"))
	require.Equal(t, 0, strings.Count(recv, "Unimplemented"))

	// One server down.
	orderers.Servers[2].Stop()
	time.Sleep(3 * time.Second)
	recv, err = sendRecv(t, stream)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(recv, "SUCCESS"), recv)
	require.Equal(t, 1, strings.Count(recv, "Unavailable"), recv)
	require.Equal(t, 0, strings.Count(recv, "Unimplemented"), recv)

	// One incorrect server.
	healthcheck := health.NewServer()
	healthcheck.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)
	fakeServer := test.RunGrpcServerForTest(
		t, &connection.ServerConfig{
			Endpoint: orderers.Configs[2].Endpoint,
		}, func(server *grpc.Server) {
			healthgrpc.RegisterHealthServer(server, healthcheck)
		},
	)
	time.Sleep(3 * time.Second)
	recv, err = sendRecv(t, stream)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(recv, "SUCCESS"), recv)
	require.Equal(t, 0, strings.Count(recv, "Unavailable"), recv)
	require.Equal(t, 1, strings.Count(recv, "Unimplemented"), recv)

	// All good again.
	fakeServer.Stop()
	orderers.Servers[2] = test.RunGrpcServerForTest(t, orderers.Configs[2], func(server *grpc.Server) {
		ab.RegisterAtomicBroadcastServer(server, mocks[2])
	})
	time.Sleep(3 * time.Second)
	recv, err = sendRecv(t, stream)
	require.NoError(t, err)
	require.Equal(t, 3, strings.Count(recv, "SUCCESS"), recv)
	require.Equal(t, 0, strings.Count(recv, "Unavailable"), recv)
	require.Equal(t, 0, strings.Count(recv, "Unimplemented"), recv)

	orderers.Servers[0].Stop()
	orderers.Servers[1].Stop()
	time.Sleep(3 * time.Second)

	recv, err = sendRecv(t, stream)
	// Insufficient quorum.
	require.Error(t, err)
	require.Equal(t, 1, strings.Count(recv, "SUCCESS"), recv)
	require.Equal(t, 2, strings.Count(recv, "Unavailable"), recv)
	require.Equal(t, 0, strings.Count(recv, "Unimplemented"), recv)
}

func sendRecv(t *testing.T, stream *EnvelopedStream) (string, error) {
	tx := &protoblocktx.Tx{}
	_, resp, err := stream.SubmitWithEnv(protoutil.MarshalOrPanic(tx))
	if err != nil {
		t.Logf("Response error: %s", err)
		return err.Error(), err
	}
	t.Logf("Response info: %s", resp.Info)
	require.Equal(t, common.Status_SUCCESS, resp.Status)
	return resp.Info, err
}

func makeConfig(t *testing.T) ([]*mock.Orderer, *test.GrpcServers, Config) {
	logging.SetupWithConfig(&logging.Config{
		Enabled:     true,
		Level:       logging.Info,
		Caller:      true,
		Development: true,
	})

	orgCount := 3
	ordererService, ordererServer := mock.StartMockOrderingServices(
		t, orgCount, mock.OrdererConfig{BlockSize: 100},
	)
	require.Len(t, ordererServer.Servers, orgCount)

	conf := Config{
		ChannelID:       "mychannel",
		ConsensusType:   Bft,
		SignedEnvelopes: false,
		Retry: &connection.RetryProfile{
			MaxElapsedTime: time.Second,
		},
	}
	for i, c := range ordererServer.Configs {
		conf.Endpoints = append(conf.Endpoints, &connection.OrdererEndpoint{
			ID:       uint32(i), //nolint:gosec // integer overflow conversion int -> uint32
			Endpoint: c.Endpoint,
		})
	}
	return ordererService, ordererServer, conf
}
