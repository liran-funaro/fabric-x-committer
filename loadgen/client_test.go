package loadgen

import (
	_ "embed"
	"net/http"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/coordinator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/vc"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/verifier"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/broadcastdeliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

// We expect at least 3 blocks for a valid test run.
const defaultExpectedTXs = defaultBlockSize * 3

func TestLoadGenForVCService(t *testing.T) {
	t.Parallel()
	env := vc.NewValidatorAndCommitServiceTestEnv(t, 2)
	clientConf := DefaultClientConf()
	clientConf.Adapter.VCClient = &adapters.VCClientConfig{
		Endpoints: env.Endpoints,
	}
	testLoadGenerator(t, clientConf)
}

func TestLoadGenForSigVerifier(t *testing.T) {
	t.Parallel()
	endpoints := make([]*connection.Endpoint, 2)
	for i := range endpoints {
		sConf := &verifier.Config{
			Server: connection.NewLocalHostServer(),
			ParallelExecutor: verifier.ExecutorConfig{
				BatchSizeCutoff:   50,
				BatchTimeCutoff:   10 * time.Millisecond,
				ChannelBufferSize: 50,
				Parallelism:       40,
			},
		}

		service := verifier.New(sConf)
		test.RunGrpcServerForTest(t.Context(), t, sConf.Server, func(server *grpc.Server) {
			protosigverifierservice.RegisterVerifierServer(server, service)
		})
		endpoints[i] = &sConf.Server.Endpoint
	}

	// Start client
	clientConf := DefaultClientConf()
	clientConf.Adapter.SigVerifierClient = &adapters.SVClientConfig{
		Endpoints: endpoints,
	}
	testLoadGenerator(t, clientConf)
}

func TestLoadGenForCoordinator(t *testing.T) {
	t.Parallel()
	_, sigVerServer := mock.StartMockSVService(t, 1)
	_, vcServer := mock.StartMockVCService(t, 1)

	cConf := &coordinator.Config{
		ServerConfig: connection.NewLocalHostServer(),
		Monitoring:   defaultMonitoring(),
		SignVerifierConfig: &coordinator.SignVerifierConfig{
			ServerConfig: []*connection.ServerConfig{
				{
					Endpoint: sigVerServer.Configs[0].Endpoint,
				},
			},
		},
		ValidatorCommitterConfig: &coordinator.ValidatorCommitterConfig{
			ServerConfig: []*connection.ServerConfig{
				{
					Endpoint: vcServer.Configs[0].Endpoint,
				},
			},
		},
		DependencyGraphConfig: &coordinator.DependencyGraphConfig{
			NumOfLocalDepConstructors:       1,
			WaitingTxsLimit:                 10_000,
			NumOfWorkersForGlobalDepManager: 1,
		},
		ChannelBufferSizePerGoroutine: 10,
	}

	service := coordinator.NewCoordinatorService(cConf)
	test.RunServiceAndGrpcForTest(t.Context(), t, service, cConf.ServerConfig, func(server *grpc.Server) {
		protocoordinatorservice.RegisterCoordinatorServer(server, service)
	})

	// Start client
	clientConf := DefaultClientConf()
	clientConf.Adapter.CoordinatorClient = &adapters.CoordinatorClientConfig{
		Endpoint: &cConf.ServerConfig.Endpoint,
	}
	testLoadGenerator(t, clientConf)
}

func TestLoadGenForSidecar(t *testing.T) {
	t.Parallel()

	_, coordinatorServer := mock.StartMockCoordinatorService(t)

	// When using the sidecar adapter, the load generator and the sidecar
	// should have each other's endpoints.
	// To avoid manually pre-choosing ports that might conflict with other tests,
	// we pre allocate them by starting a listener that picks a port automatically and bind to it.
	// In real evaluation scenario, the ports will be selected by the deployment infrastructure.
	sidecarServerConf := connection.NewLocalHostServer()
	preAllocatePorts(t, sidecarServerConf)

	ordererServers := make([]*connection.ServerConfig, 3)
	for i := range ordererServers {
		ordererServers[i] = &connection.ServerConfig{
			Endpoint: connection.Endpoint{Host: "localhost"},
		}
		preAllocatePorts(t, ordererServers[i])
	}

	// Start server under test
	chanID := "channel"
	sidecarConf := &sidecar.Config{
		Server: sidecarServerConf,
		Orderer: broadcastdeliver.Config{
			Connection: broadcastdeliver.ConnectionConfig{
				Endpoints: connection.NewOrdererEndpoints(0, "org", ordererServers...),
			},
			ChannelID:     chanID,
			ConsensusType: broadcastdeliver.Bft,
		},
		Committer: sidecar.CoordinatorConfig{
			Endpoint: coordinatorServer.Configs[0].Endpoint,
		},
		Monitoring: defaultMonitoring(),
		Ledger: sidecar.LedgerConfig{
			Path: t.TempDir(),
		},
	}
	service, err := sidecar.New(sidecarConf)
	require.NoError(t, err)
	t.Cleanup(service.Close)
	test.RunServiceAndGrpcForTest(t.Context(), t, service, sidecarConf.Server, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, service.GetLedgerService())
	})

	// Start client
	clientConf := DefaultClientConf()
	clientConf.Adapter.SidecarClient = &adapters.SidecarClientConfig{
		SidecarEndpoint: &sidecarServerConf.Endpoint,
		ChannelID:       chanID,
		OrdererServers:  ordererServers,
	}
	testLoadGenerator(t, clientConf)
}

func TestLoadGenForOrderer(t *testing.T) {
	t.Parallel()
	// Start dependencies
	orderer, ordererServer := mock.StartMockOrderingServices(
		t, &mock.OrdererConfig{NumService: 3, BlockSize: 100},
	)
	_, coordinatorServer := mock.StartMockCoordinatorService(t)

	endpoints := connection.NewOrdererEndpoints(0, "msp", ordererServer.Configs...)
	sidecarConf := &sidecar.Config{
		Server: connection.NewLocalHostServer(),
		Orderer: broadcastdeliver.Config{
			Connection: broadcastdeliver.ConnectionConfig{
				Endpoints: endpoints,
			},
			ChannelID:     "mychannel",
			ConsensusType: broadcastdeliver.Bft,
		},
		Committer: sidecar.CoordinatorConfig{
			Endpoint: coordinatorServer.Configs[0].Endpoint,
		},
		Monitoring: defaultMonitoring(),
		Ledger: sidecar.LedgerConfig{
			Path: t.TempDir(),
		},
	}

	// Start sidecar.
	service, err := sidecar.New(sidecarConf)
	require.NoError(t, err)
	t.Cleanup(service.Close)
	test.RunServiceAndGrpcForTest(t.Context(), t, service, sidecarConf.Server, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, service.GetLedgerService())
	})

	// Submit default config block.
	clientConf := DefaultClientConf()
	require.NotNil(t, clientConf.LoadProfile)
	clientConf.LoadProfile.Transaction.Policy.OrdererEndpoints = endpoints
	configBlock, err := workload.CreateConfigBlock(clientConf.LoadProfile.Transaction.Policy)
	require.NoError(t, err)
	orderer.SubmitBlock(t.Context(), configBlock)

	// Start client
	clientConf.Adapter.OrdererClient = &adapters.OrdererClientConfig{
		SidecarEndpoint:      &sidecarConf.Server.Endpoint,
		Orderer:              sidecarConf.Orderer,
		BroadcastParallelism: 5,
	}
	testLoadGenerator(t, clientConf)
}

func preAllocatePorts(t *testing.T, server *connection.ServerConfig) {
	t.Helper()
	listener, err := server.PreAllocateListener()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
	})
}

func testLoadGenerator(t *testing.T, c *ClientConfig) {
	t.Helper()
	client := runLoadGenerator(t, c)

	test.CheckMetrics(t, &http.Client{}, metrics.GetMetricsForTests(t, client.resources.Metrics).URL, []string{
		"loadgen_block_sent_total",
		"loadgen_transaction_sent_total",
		"loadgen_transaction_received_total",
		"loadgen_valid_transaction_latency_seconds",
		"loadgen_invalid_transaction_latency_seconds",
	})

	eventuallyMetrics(t, client.resources.Metrics, func(m metrics.Values) bool {
		return m.TransactionSentTotal > defaultExpectedTXs &&
			m.TransactionReceivedTotal > defaultExpectedTXs &&
			m.TransactionCommittedTotal > defaultExpectedTXs &&
			m.TransactionAbortedTotal == 0
	})
}
