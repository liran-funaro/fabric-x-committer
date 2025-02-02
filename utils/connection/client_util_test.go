package connection_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

func TestGRPCRetry(t *testing.T) {
	commonGrpcRetryTest(t, connection.Connect)
	commonGrpcRetryTest(t, connection.LazyConnect)
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
