package loadgen

import (
	_ "embed"
	"fmt"
	"testing"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice"
	loadgenmetrics "github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

var (
	//go:embed config_template/sig_server.yaml
	sigVerifierServerOnlyTemplate string

	//go:embed config_template/sig_client.yaml
	sigVerifierClientOnlyTemplate string

	//go:embed config_template/vc_client.yaml
	vcClientOnlyTemplate string

	//go:embed config_template/coordinator_server.yaml
	coordinatorServerOnlyTemplate string

	//go:embed config_template/coordinator_client.yaml
	coordinatorClientOnlyTemplate string

	//go:embed config_template/sidecar_server.yaml
	sidecarServerOnlyTemplate string

	//go:embed config_template/sidecar_client.yaml
	sidecarClientOnlyTemplate string

	sigVerifierServerTemplate = combineServerTemplates(sigVerifierServerOnlyTemplate)
	sigVerifierClientTemplate = combineClientTemplates(sigVerifierClientOnlyTemplate)

	coordinatorServerTemplate = combineServerTemplates(coordinatorServerOnlyTemplate)
	coordinatorClientTemplate = combineClientTemplates(coordinatorClientOnlyTemplate)

	sidecarServerTemplate = combineServerTemplates(sidecarServerOnlyTemplate)
	sidecarClientTemplate = combineClientTemplates(sidecarClientOnlyTemplate)
)

func TestLoadGenForVCService(t *testing.T) {
	// Start dependencies
	activateVcServiceForTest(t, []int{9002, 9003}, []int{10001, 10002})

	// Start client
	loadConfigString(
		t,
		combineClientTemplates(vcClientOnlyTemplate),
		tempFile(t, "client-log.txt"),
		2110,
		9002,
		9003,
	)
	testLoadGenerator(t, ReadConfig(), func(m loadgenmetrics.Values) bool {
		return m.TransactionSentTotal > 100 &&
			m.TransactionReceivedTotal > 100 &&
			m.TransactionCommittedTotal > 100 &&
			// We do not expect aborted TXs because the VC does not verify the TXs.
			m.TransactionAbortedTotal == 0
	})
}

func TestLoadGenForSigVerifier(t *testing.T) {
	for i := 0; i < 2; i++ {
		// Start server under test
		loadConfigString(
			t,
			sigVerifierServerTemplate,
			tempFile(t, fmt.Sprintf("server-log-%d.yaml", i)),
			2110+i,
			5000+i,
		)
		conf := serverconfig.ReadConfig()

		service := verifierserver.New(&conf.ParallelExecutor, &metrics.Metrics{Enabled: false})
		test.RunGrpcServerForTest(t, conf.Server, func(server *grpc.Server) {
			protosigverifierservice.RegisterVerifierServer(server, service)
		})
	}

	// Start client
	loadConfigString(t, sigVerifierClientTemplate, tempFile(t, "client-log.txt"), 2112, 5000, 5001)
	testLoadGenerator(t, ReadConfig(), func(m loadgenmetrics.Values) bool {
		return m.TransactionSentTotal > 100 &&
			m.TransactionReceivedTotal > 100 &&
			m.TransactionCommittedTotal > 100 &&
			m.TransactionAbortedTotal > 0
	})
}

func TestLoadGenForCoordinator(t *testing.T) { // nolint: gocognit
	// Start dependencies
	_, sigVerServer := mock.StartMockSVService(t, 1)
	_, vcServer := mock.StartMockVCService(t, 1)

	// Start server under test
	loadConfigString(t, coordinatorServerTemplate, tempFile(t, "client-log.txt"),
		sigVerServer.Configs[0].Endpoint.Port, vcServer.Configs[0].Endpoint.Port, 2110, 9001)
	conf := coordinatorservice.ReadConfig()

	service := coordinatorservice.NewCoordinatorService(conf)
	test.RunServiceAndGrpcForTest(t, service, conf.ServerConfig, func(server *grpc.Server) {
		protocoordinatorservice.RegisterCoordinatorServer(server, service)
	})

	// Start client
	loadConfigString(t, coordinatorClientTemplate, tempFile(t, "client-log.txt"), 2112, 9001)
	testLoadGenerator(t, ReadConfig(), func(m loadgenmetrics.Values) bool {
		return m.TransactionSentTotal > 100 &&
			m.TransactionReceivedTotal > 100 &&
			m.TransactionCommittedTotal > 100 &&
			// We do not expect aborted TXs because we use mock sig ver.
			m.TransactionAbortedTotal == 0
	})
}

func TestLoadGenForSidecar(t *testing.T) { // nolint: gocognit
	// Start dependencies
	_, ordererServer := mock.StartMockOrderingServices(
		t, 3, mock.OrdererConfig{BlockSize: 100},
	)
	_, coordinatorServer := mock.StartMockCoordinatorService(t)

	// Start server under test
	ledgerPath := tempFile(t, "ledger")
	loadConfigString(t,
		sidecarServerTemplate,
		tempFile(t, "server-log.txt"),
		ordererServer.Configs[0].Endpoint.Port,
		coordinatorServer.Configs[0].Endpoint.Port,
		ledgerPath,
		2110,
		9001,
	)
	sidecarConf := sidecar.ReadConfig()
	service, err := sidecar.New(&sidecarConf)
	require.NoError(t, err)
	t.Cleanup(service.Close)
	test.RunServiceAndGrpcForTest(t, service, sidecarConf.Server, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, service.GetLedgerService())
	})

	// Start client
	loadConfigString(
		t,
		sidecarClientTemplate,
		tempFile(t, "client-log.txt"),
		2111,
		sidecarConf.Server.Endpoint.Port,
		coordinatorServer.Configs[0].Endpoint.Port,
		ordererServer.Configs[0].Endpoint.Port,
		ordererServer.Configs[1].Endpoint.Port,
		ordererServer.Configs[2].Endpoint.Port,
	)
	testLoadGenerator(t, ReadConfig(), func(m loadgenmetrics.Values) bool {
		return m.TransactionSentTotal > 100 &&
			m.TransactionReceivedTotal > 100 &&
			m.TransactionCommittedTotal > 100 &&
			// We do not expect aborted TXs because we use mock sig ver.
			m.TransactionAbortedTotal == 0
	})
}
