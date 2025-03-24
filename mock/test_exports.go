package mock

import (
	"context"
	"testing"

	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

// StartMockSVService starts a specified number of mock verifier service and register cancellation.
func StartMockSVService(t *testing.T, numService int) (
	[]*SigVerifier, *test.GrpcServers,
) {
	mockSigVer := make([]*SigVerifier, numService)
	for i := 0; i < numService; i++ {
		mockSigVer[i] = NewMockSigVerifier()
	}

	sigVerServers := test.StartGrpcServersForTest(context.Background(), t, len(mockSigVer),
		func(server *grpc.Server, index int) {
			protosigverifierservice.RegisterVerifierServer(server, mockSigVer[index])
		})
	return mockSigVer, sigVerServers
}

// StartMockSVServiceFromListWithConfig starts a specified number of mock verifier service.
func StartMockSVServiceFromListWithConfig(
	t *testing.T, svs []*SigVerifier, sc []*connection.ServerConfig,
) *test.GrpcServers {
	return test.StartGrpcServersWithConfigForTest(context.Background(), t, sc, func(server *grpc.Server, index int) {
		protosigverifierservice.RegisterVerifierServer(server, svs[index])
	})
}

// StartMockVCService starts a specified number of mock VC service and register cancellation.
func StartMockVCService(t *testing.T, numService int) (
	[]*VcService, *test.GrpcServers,
) {
	vcServices := make([]*VcService, numService)
	for i := 0; i < numService; i++ {
		vcServices[i] = NewMockVcService()
	}

	vcGrpc := test.StartGrpcServersForTest(context.Background(), t, numService, func(server *grpc.Server, index int) {
		protovcservice.RegisterValidationAndCommitServiceServer(server, vcServices[index])
	})
	return vcServices, vcGrpc
}

// StartMockVCServiceFromListWithConfig starts a specified number of mock vc service.
func StartMockVCServiceFromListWithConfig(
	t *testing.T, vcs []*VcService, sc []*connection.ServerConfig,
) *test.GrpcServers {
	return test.StartGrpcServersWithConfigForTest(context.Background(), t, sc, func(server *grpc.Server, index int) {
		protovcservice.RegisterValidationAndCommitServiceServer(server, vcs[index])
	})
}

// StartMockCoordinatorService starts a mock coordinator service and registers cancellation.
func StartMockCoordinatorService(t *testing.T) (
	*Coordinator, *test.GrpcServers,
) {
	mockCoordinator := NewMockCoordinator()
	t.Cleanup(mockCoordinator.Close)
	coordinatorGrpc := test.StartGrpcServersForTest(context.Background(), t, 1, func(server *grpc.Server, _ int) {
		protocoordinatorservice.RegisterCoordinatorServer(server, mockCoordinator)
	})
	return mockCoordinator, coordinatorGrpc
}

// StartMockCoordinatorServiceFromListWithConfig starts a mock coordinator service using the given config.
func StartMockCoordinatorServiceFromListWithConfig(
	t *testing.T,
	coordService *Coordinator,
	sc *connection.ServerConfig,
) *test.GrpcServers {
	t.Helper()
	return test.StartGrpcServersWithConfigForTest(t.Context(), t, []*connection.ServerConfig{sc},
		func(server *grpc.Server, _ int) {
			protocoordinatorservice.RegisterCoordinatorServer(server, coordService)
		})
}

// StartMockOrderingServices starts a specified number of mock ordering service and register cancellation.
func StartMockOrderingServices(t *testing.T, conf *OrdererConfig) (
	*Orderer, *test.GrpcServers,
) {
	service, err := NewMockOrderer(conf)
	require.NoError(t, err)
	test.RunServiceForTest(t.Context(), t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(service.Run(ctx))
	}, service.WaitForReady)

	if len(conf.ServerConfigs) == conf.NumService {
		return service, test.StartGrpcServersWithConfigForTest(
			context.Background(),
			t, conf.ServerConfigs, func(server *grpc.Server, index int) {
				ab.RegisterAtomicBroadcastServer(server, service)
			},
		)
	}

	servers := test.StartGrpcServersForTest(
		context.Background(), t, conf.NumService, func(server *grpc.Server, index int) {
			ab.RegisterAtomicBroadcastServer(server, service)
		},
	)
	return service, servers
}
