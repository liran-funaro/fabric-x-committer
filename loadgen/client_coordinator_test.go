package loadgen

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

const (
	coordinatorServerTemplate = loggingTemplate + `
coordinator-service:` + serverTemplate + `
  sign-verifier:
    server:
      - # server 1 configuration
        endpoint: localhost:%d
  validator-committer:
    server:
      - # server 1 configuration
        endpoint: localhost:%d
  dependency-graph:
    num-of-local-dep-constructors: 1
    waiting-txs-limit: 10000
    num-of-workers-for-global-dep-manager: 1
  per-channel-buffer-size-per-goroutine: 10
`
	coordinatorClientTemplate = clientTemplate + `
coordinator-client:
  endpoint: localhost:%d
`
)

func TestBlockGenForCoordinator(t *testing.T) { // nolint: gocognit
	// Start dependencies
	sigVerServerConfig, mockSigVer, sigVerGrpc := sigverifiermock.StartMockSVService(1)
	vcServerConfig, mockVC, vcGrpc := vcservicemock.StartMockVCService(1)
	t.Cleanup(func() {
		for _, sv := range mockSigVer {
			sv.Close()
		}
		for _, vc := range mockVC {
			vc.Close()
		}
		for _, svGrpc := range sigVerGrpc {
			svGrpc.Stop()
		}
		for _, svGrpc := range vcGrpc {
			svGrpc.Stop()
		}
	})

	// Start server under test
	loadConfig(t, "server-config.yaml", coordinatorServerTemplate, tempFile(t, "client-log.txt"), 2110, 9001, sigVerServerConfig[0].Endpoint.Port, vcServerConfig[0].Endpoint.Port)
	conf := coordinatorservice.ReadConfig()

	service := coordinatorservice.NewCoordinatorService(conf)
	_, err := service.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	server, _ := startServer(*conf.ServerConfig, func(server *grpc.Server) {
		protocoordinatorservice.RegisterCoordinatorServer(server, service)
	})
	t.Cleanup(func() { server.Stop() })

	// Start client
	loadConfig(t, "client-config.yaml", coordinatorClientTemplate, tempFile(t, "client-log.txt"), 2112, 9001)
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
		return test.GetMetricValue(t, metrics.transactionSentTotal) ==
			test.GetMetricValue(t, metrics.transactionReceivedTotal)
	}, 20*time.Second, 500*time.Millisecond)
}
