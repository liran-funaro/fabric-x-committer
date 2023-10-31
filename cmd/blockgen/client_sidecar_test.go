package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/coordinatormock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/sidecarservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

const (
	sidecarServerTemplate = loggingTemplate + `
sidecar:` + serverTemplate + `
  orderer:
    endpoint: localhost:%d
    channel-id: mychannel
    reconnect: 10s
  committer:
    endpoint: localhost:%d
  ledger:
    path: %s
`
	sidecarClientTemplate = clientTemplate + `
sidecar-client:
  endpoint: localhost:%d
  coordinator:
    endpoint: localhost:%d
  orderer:
    endpoints:
      - localhost:%d
      - localhost:%d
      - localhost:%d
    channel-id: mychannel
    type: BFT
    signed-envelopes: false
    parallelism: 50
`
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
	loadConfig(t, "server-config.yaml", sidecarServerTemplate, tempFile(t, "server-log.txt"), 2110, 9001, ordererServerConfig[0].Endpoint.Port, coordinatorServerConfig.Endpoint.Port, ledgerPath)
	conf := sidecarservice.ReadConfig()

	service, err := sidecarservice.NewService(&conf)
	require.NoError(t, err)
	_, _, _, err = service.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	server, sidecarServerConfig := startServer(*conf.Server, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, service)
	})
	t.Cleanup(func() {
		logger.Infof("cleaning up")
		server.Stop()
	})

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
