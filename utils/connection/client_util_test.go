package connection_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCRetry(t *testing.T) {
	t.Parallel()
	t.Run("eager connect", func(t *testing.T) {
		t.Parallel()
		commonGrpcRetryTest(t, connection.Connect)
	})
	t.Run("lazy connect", func(t *testing.T) {
		t.Parallel()
		commonGrpcRetryTest(t, connection.LazyConnect)
	})
}

func commonGrpcRetryTest(
	t *testing.T,
	connectionSetup func(config *connection.DialConfig) (*grpc.ClientConn, error),
) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	regService := func(server *grpc.Server, _ int) {
		protovcservice.RegisterValidationAndCommitServiceServer(server, mock.NewMockVcService())
	}

	vcGrpc := test.StartGrpcServersForTest(ctx, t, 1, regService)

	conn, err := connectionSetup(connection.NewDialConfig(&vcGrpc.Configs[0].Endpoint))
	require.NoError(t, err)

	client := protovcservice.NewValidationAndCommitServiceClient(conn)
	w, err := client.NumberOfWaitingTransactionsForStatus(ctx, nil)
	require.NoError(t, err)
	require.Zero(t, w.Count)

	cancel() // stopping the grpc server

	// ensure that the server is down
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel2()
	require.Eventually(t, func() bool {
		_, err := client.NumberOfWaitingTransactionsForStatus(ctx2, nil)
		return err != nil
	}, 5*time.Second, 250*time.Millisecond)
	time.Sleep(5 * time.Second)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		require.Eventually(t, func() bool {
			_, err := client.NumberOfWaitingTransactionsForStatus(ctx2, nil)
			return err == nil
		}, 2*time.Minute, 5*time.Second)
	}()

	time.Sleep(30 * time.Second)
	test.StartGrpcServersWithConfigForTest(ctx2, t, vcGrpc.Configs, regService)

	wg.Wait()
}

type fakeBroadcastDeliver struct{}

func (fakeBroadcastDeliver) Deliver(stream peer.Deliver_DeliverServer) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		switch len(msg.Payload) {
		case 0:
			return errors.New("bad env")
		case 1:
			return nil
		}
		err = stream.Send(&peer.DeliverResponse{})
		if err != nil {
			return err
		}
	}
}

func (fakeBroadcastDeliver) DeliverFiltered(peer.Deliver_DeliverFilteredServer) error {
	panic("not implemented")
}

func (fakeBroadcastDeliver) DeliverWithPrivateData(peer.Deliver_DeliverWithPrivateDataServer) error {
	panic("not implemented")
}

var (
	badEnv = &common.Envelope{}
	endEnv = &common.Envelope{
		Payload: make([]byte, 1),
	}
	goodEnv = &common.Envelope{
		Payload: make([]byte, 2),
	}
)

type filterTestEnv struct {
	service       *fakeBroadcastDeliver
	serverConf    *connection.ServerConfig
	server        *grpc.Server
	client        peer.DeliverClient
	deliver       peer.Deliver_DeliverClient
	serviceCancel context.CancelFunc
	clientCancel  context.CancelFunc
}

func newFilterTestEnv(t *testing.T) *filterTestEnv {
	env := &filterTestEnv{
		service:    &fakeBroadcastDeliver{},
		serverConf: &connection.ServerConfig{Endpoint: connection.Endpoint{Host: "localhost"}},
	}

	serviceCtx, serviceCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(serviceCancel)
	env.serviceCancel = serviceCancel

	env.server = test.RunGrpcServerForTest(serviceCtx, t, env.serverConf, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, env.service)
	})
	conn, err := connection.Connect(connection.NewDialConfig(&env.serverConf.Endpoint))
	require.NoError(t, err)
	env.client = peer.NewDeliverClient(conn)

	clientCtx, clientCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(clientCancel)
	env.clientCancel = clientCancel
	env.deliver, err = env.client.Deliver(clientCtx)
	require.NoError(t, err)

	// Sanity check.
	err = env.deliver.Send(goodEnv)
	require.NoError(t, err)
	_, err = env.deliver.Recv()
	require.NoError(t, err)
	requireNoWrappedError(t, err)

	return env
}

func TestFilterStreamRPCError(t *testing.T) {
	t.Parallel()

	t.Run("EOF", func(t *testing.T) {
		t.Parallel()
		env := newFilterTestEnv(t)
		err := env.deliver.Send(endEnv)
		require.NoError(t, err)
		_, err = env.deliver.Recv()
		require.ErrorIs(t, err, io.EOF)
		requireNoWrappedError(t, err)
	})

	t.Run("client ctx cancel", func(t *testing.T) {
		t.Parallel()
		env := newFilterTestEnv(t)
		env.clientCancel()
		_, err := env.deliver.Recv()
		require.Error(t, err)
		requireNoWrappedError(t, err)
		requireErrorIsRPC(t, err, codes.Canceled)
	})

	t.Run("client ctx timeout", func(t *testing.T) {
		t.Parallel()
		clientCtx, clientCancel := context.WithTimeout(context.Background(), time.Second)
		t.Cleanup(clientCancel)
		env := newFilterTestEnv(t)
		deliver, err := env.client.Deliver(clientCtx)
		require.NoError(t, err)
		time.Sleep(time.Second)
		_, err = deliver.Recv()
		require.Error(t, err)
		requireNoWrappedError(t, err)
		requireErrorIsRPC(t, err, codes.DeadlineExceeded)
	})

	t.Run("with error", func(t *testing.T) {
		t.Parallel()
		env := newFilterTestEnv(t)
		err := env.deliver.Send(badEnv)
		require.NoError(t, err)
		_, err = env.deliver.Recv()
		require.Error(t, err)
		require.Error(t, connection.FilterStreamRPCError(err))
		require.Error(t, connection.FilterStreamRPCError(errors.Join(err, errors.New("failed"))))
		require.Error(t, connection.FilterStreamRPCError(fmt.Errorf("failed: %w", err)))
	})

	t.Run("server ctx cancel", func(t *testing.T) {
		t.Parallel()
		env := newFilterTestEnv(t)
		wg := sync.WaitGroup{}
		wg.Add(1)
		defer wg.Wait()
		go func() {
			defer wg.Done()
			_, err := env.deliver.Recv()
			require.Error(t, err)
			// This returns either codes.Canceled or codes.Unavailable (EOF).
			requireNoWrappedError(t, err)
		}()
		time.Sleep(3 * time.Second)
		env.serviceCancel()
	})

	t.Run("server shutdown", func(t *testing.T) {
		t.Parallel()
		env := newFilterTestEnv(t)
		wg := sync.WaitGroup{}
		wg.Add(1)
		defer wg.Wait()
		go func() {
			defer wg.Done()
			_, err := env.deliver.Recv()
			require.Error(t, err)
			// This returns either codes.Canceled or codes.Unavailable (EOF).
			requireNoWrappedError(t, err)
		}()
		time.Sleep(3 * time.Second)
		env.server.Stop()
	})
}

func requireNoWrappedError(t *testing.T, err error) {
	require.NoError(t, connection.FilterStreamRPCError(err))
	if err == nil {
		return
	}
	require.NoError(t, connection.FilterStreamRPCError(errors.Join(err, errors.New("failed"))))
	require.NoError(t, connection.FilterStreamRPCError(fmt.Errorf("failed: %w", err)))
}

func requireErrorIsRPC(t *testing.T, rpcErr error, code codes.Code) {
	errStatus, ok := status.FromError(rpcErr)
	require.True(t, ok)
	rpcErrCode := errStatus.Code()
	require.Equal(t, code, rpcErrCode)
}
