package loadgen

import (
	_ "embed"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/broadcastdeliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters"
	loadgenmetrics "github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

func TestLoadGenForVCService(t *testing.T) {
	t.Parallel()
	_, endpoints := activateVcServiceForTest(t, 2)
	clientConf := defaultClientConf()
	clientConf.Adapter.VCClient = &adapters.VCClientConfig{
		Endpoints: endpoints,
	}
	testLoadGenerator(t, clientConf, func(m loadgenmetrics.Values) bool {
		return m.TransactionSentTotal > 100 &&
			m.TransactionReceivedTotal > 100 &&
			m.TransactionCommittedTotal > 100 &&
			// We do not expect aborted TXs because the VC does not verify the TXs.
			m.TransactionAbortedTotal == 0
	})
}

func TestLoadGenForSigVerifier(t *testing.T) {
	t.Parallel()
	endpoints := make([]*connection.Endpoint, 2)
	for i := range endpoints {
		sConf := &serverconfig.SigVerificationConfig{
			Server: defaultServer(),
			ParallelExecutor: parallelexecutor.Config{
				BatchSizeCutoff:   50,
				BatchTimeCutoff:   10 * time.Millisecond,
				ChannelBufferSize: 50,
				Parallelism:       40,
			},
		}

		service := verifierserver.New(&sConf.ParallelExecutor, &metrics.Metrics{Enabled: false})
		test.RunGrpcServerForTest(t.Context(), t, sConf.Server, func(server *grpc.Server) {
			protosigverifierservice.RegisterVerifierServer(server, service)
		})
		endpoints[i] = &sConf.Server.Endpoint
	}

	// Start client
	clientConf := defaultClientConf()
	clientConf.Adapter.SigVerifierClient = &adapters.SVClientConfig{
		Endpoints: endpoints,
	}
	testLoadGenerator(t, clientConf, func(m loadgenmetrics.Values) bool {
		return m.TransactionSentTotal > 100 &&
			m.TransactionReceivedTotal > 100 &&
			m.TransactionCommittedTotal > 100 &&
			m.TransactionAbortedTotal > 0
	})
}

func TestLoadGenForCoordinator(t *testing.T) { // nolint: gocognit
	t.Parallel()
	_, sigVerServer := mock.StartMockSVService(t, 1)
	_, vcServer := mock.StartMockVCService(t, 1)

	cConf := &coordinatorservice.CoordinatorConfig{
		ServerConfig: defaultServer(),
		Monitoring:   defaultMonitoring(),
		SignVerifierConfig: &coordinatorservice.SignVerifierConfig{
			ServerConfig: []*connection.ServerConfig{
				{
					Endpoint: sigVerServer.Configs[0].Endpoint,
				},
			},
		},
		ValidatorCommitterConfig: &coordinatorservice.ValidatorCommitterConfig{
			ServerConfig: []*connection.ServerConfig{
				{
					Endpoint: vcServer.Configs[0].Endpoint,
				},
			},
		},
		DependencyGraphConfig: &coordinatorservice.DependencyGraphConfig{
			NumOfLocalDepConstructors:       1,
			WaitingTxsLimit:                 10_000,
			NumOfWorkersForGlobalDepManager: 1,
		},
		ChannelBufferSizePerGoroutine: 10,
	}

	service := coordinatorservice.NewCoordinatorService(cConf)
	test.RunServiceAndGrpcForTest(t.Context(), t, service, cConf.ServerConfig, func(server *grpc.Server) {
		protocoordinatorservice.RegisterCoordinatorServer(server, service)
	})

	// Start client
	clientConf := defaultClientConf()
	clientConf.Adapter.CoordinatorClient = &adapters.CoordinatorClientConfig{
		Endpoint: &cConf.ServerConfig.Endpoint,
	}
	testLoadGenerator(t, clientConf, func(m loadgenmetrics.Values) bool {
		return m.TransactionSentTotal > 100 &&
			m.TransactionReceivedTotal > 100 &&
			m.TransactionCommittedTotal > 100 &&
			// We do not expect aborted TXs because we use mock sig ver.
			m.TransactionAbortedTotal == 0
	})
}

func TestLoadGenForSidecar(t *testing.T) { // nolint: gocognit
	t.Parallel()
	_, ordererServer := mock.StartMockOrderingServices(
		t, &mock.OrdererConfig{NumService: 3, BlockSize: 100},
	)
	_, coordinatorServer := mock.StartMockCoordinatorService(t)

	endpoints := make([]*connection.OrdererEndpoint, len(ordererServer.Configs))
	for i, c := range ordererServer.Configs {
		endpoints[i] = &connection.OrdererEndpoint{
			Endpoint: c.Endpoint,
			ID:       uint32(i + 1), //nolint:gosec // integer overflow conversion int -> uint32
		}
	}

	ordererConfig := broadcastdeliver.Config{
		Connection: broadcastdeliver.ConnectionConfig{
			Endpoints: endpoints,
		},
		ChannelID:     "mychannel",
		ConsensusType: broadcastdeliver.Bft,
	}
	sidecarConf := &sidecar.Config{
		Server:     defaultServer(),
		Monitoring: defaultMonitoring(),
		Orderer:    ordererConfig,
		Committer: sidecar.CoordinatorConfig{
			Endpoint: coordinatorServer.Configs[0].Endpoint,
		},
		Ledger: sidecar.LedgerConfig{
			Path: t.TempDir(),
		},
	}

	// Start server under test
	service, err := sidecar.New(sidecarConf)
	require.NoError(t, err)
	t.Cleanup(service.Close)
	test.RunServiceAndGrpcForTest(t.Context(), t, service, sidecarConf.Server, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, service.GetLedgerService())
	})

	// Start client
	clientConf := defaultClientConf()
	clientConf.Adapter.SidecarClient = &adapters.SidecarClientConfig{
		Endpoint: &sidecarConf.Server.Endpoint,
		Coordinator: &adapters.CoordinatorClientConfig{
			Endpoint: &coordinatorServer.Configs[0].Endpoint,
		},
		Orderer:              ordererConfig,
		BroadcastParallelism: 5,
	}
	testLoadGenerator(t, clientConf, func(m loadgenmetrics.Values) bool {
		return m.TransactionSentTotal > 100 &&
			m.TransactionReceivedTotal > 100 &&
			m.TransactionCommittedTotal > 100 &&
			// We do not expect aborted TXs because we use mock sig ver.
			m.TransactionAbortedTotal == 0
	})
}
