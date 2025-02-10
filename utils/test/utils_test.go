package test_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

func TestCheckServerStopped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	mockSigVer := mock.NewMockSigVerifier()

	sigVerServers := test.StartGrpcServersForTest(ctx, t, 1,
		func(server *grpc.Server, _ int) {
			protosigverifierservice.RegisterVerifierServer(server, mockSigVer)
		})

	addr := sigVerServers.Configs[0].Endpoint.Address()
	require.Eventually(t, func() bool {
		return !test.CheckServerStopped(t, addr)
	}, 3*time.Second, 250*time.Millisecond)

	sigVerServers.Servers[0].Stop()

	require.Eventually(t, func() bool {
		return test.CheckServerStopped(t, addr)
	}, 3*time.Second, 250*time.Millisecond)
}
