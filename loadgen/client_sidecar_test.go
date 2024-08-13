package loadgen

import (
	"context"
	_ "embed"
	"net/http"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/coordinatormock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

//go:embed config_template/sidecar_server.yaml
var sidecarServerOnlyTemplate string

//go:embed config_template/sidecar_client.yaml
var sidecarClientOnlyTemplate string

var (
	sidecarServerTemplate = loggingTemplate + sidecarServerOnlyTemplate + serverTemplate
	sidecarClientTemplate = loggingTemplate + clientOnlyTemplate + sidecarClientOnlyTemplate
)

func TestBlockGenForSidecar(t *testing.T) { // nolint: gocognit
	// Start dependencies
	ordererServerConfig, mockOrderer, ordererGrpc := StartMockOrderingService(3)
	coordinatorServerConfig, mockCoordinator, coordinatorGrpc := coordinatormock.StartMockCoordinatorService()
	t.Cleanup(func() {
		for _, o := range mockOrderer {
			o.Close()
		}
		mockCoordinator.Close()
		for _, oGrpc := range ordererGrpc {
			oGrpc.Stop()
		}
		coordinatorGrpc.Stop()
	})
	// Start server under test
	ledgerPath := tempFile(t, "ledger")
	loadConfig(t,
		"server-config.yaml",
		sidecarServerTemplate,
		tempFile(t, "server-log.txt"),
		ordererServerConfig[0].Endpoint.Port,
		coordinatorServerConfig.Endpoint.Port,
		ledgerPath,
		2110,
		9001,
	)
	conf := sidecar.ReadConfig()
	service, err := sidecar.New(&conf)
	require.NoError(t, err)
	_, _, _, err = service.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	server, sidecarServerConfig := startServer(*conf.Server, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, service.LedgerService)
	})
	t.Cleanup(server.Stop)

	// Start client
	loadConfig(t, "client-config.yaml", sidecarClientTemplate, tempFile(t, "client-log.txt"), 2111, sidecarServerConfig.Port, coordinatorServerConfig.Endpoint.Port, ordererServerConfig[0].Endpoint.Port, ordererServerConfig[1].Endpoint.Port, ordererServerConfig[2].Endpoint.Port)
	metrics := startLoadGenerator(t, ReadConfig())

	// Check results
	test.CheckMetrics(t, &http.Client{}, metrics.provider.URL(), []string{
		"blockgen_block_sent_total",
		"blockgen_transaction_sent_total",
		"blockgen_transaction_received_total",
		"blockgen_valid_transaction_latency_seconds",
		"blockgen_invalid_transaction_latency_seconds",
	})

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) > 0 &&
			test.GetMetricValue(t, metrics.transactionReceivedTotal) > 0
	}, 20*time.Second, 500*time.Millisecond)
}
