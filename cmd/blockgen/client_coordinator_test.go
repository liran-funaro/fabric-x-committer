package main

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

var coordinatorLogger = logging.New("test-logger")

const (
	configTemplate = `
logging:
  enabled: true
  level: debug
  Caller: false
  Development: true
  Output: %s
coordinator-service:
  server:
    endpoint:
      host: "localhost"
      port: 9001
  sign-verifier:
    server:
      - # server 1 configuration
        endpoint:
          host: "localhost" # The host of the server
          port: %d        # The port of the server
  validator-committer:
    server:
      - # server 1 configuration
        endpoint:
          host: "localhost" # The host of the server
          port: %d        # The port of the server
  dependency-graph:
    num-of-local-dep-constructors: 1
    waiting-txs-limit: 10000
    num-of-workers-for-global-dep-manager: 1
  per-channel-buffer-size-per-goroutine: 10
  monitoring:
    metrics:
      enable: true
      endpoint: :2110
`

	testConfigFilePath                = "./test-config.yaml"
	coordinatorBlockGenConfigFilePath = "../../config/config-blockgenforcoordinator.yaml"
)

func TestBlockGenForCoordinator(t *testing.T) { // nolint: gocognit
	sigVerServerConfig, mockSigVer, sigVerGrpc := sigverifiermock.StartMockSVService(1)
	vcServerConfig, mockVC, vcGrpc := vcservicemock.StartMockVCService(1)
	var coordService *coordinatorservice.CoordinatorService
	var coordGrpcServer *grpc.Server
	var metrics *perfMetrics

	t.Cleanup(func() {
		require.NoError(t, coordService.Close())

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

		coordGrpcServer.Stop()

		require.NoError(t, metrics.provider.StopServer())
	})

	output := filepath.Clean(path.Join(t.TempDir(), "logger-output.txt"))
	conf := fmt.Sprintf(configTemplate, output, sigVerServerConfig[0].Endpoint.Port, vcServerConfig[0].Endpoint.Port)
	require.NoError(t, os.WriteFile(testConfigFilePath, []byte(conf), 0o600))

	logCnf := &logging.Config{
		Enabled:     true,
		Level:       logging.Info,
		Caller:      false,
		Development: true,
		Output:      output,
	}
	logging.SetupWithConfig(logCnf)

	t.Cleanup(func() {
		require.NoError(t, os.Remove(output))
		require.NoError(t, os.Remove(testConfigFilePath))
	})

	require.NoError(t, config.ReadYamlConfigs([]string{testConfigFilePath}))
	coordConfig := coordinatorservice.ReadConfig()
	coordService = coordinatorservice.NewCoordinatorService(coordConfig)
	_, _, err := coordService.Start()
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		connection.RunServerMain(coordConfig.ServerConfig, func(server *grpc.Server, port int) {
			if coordConfig.ServerConfig.Endpoint.Port == 0 {
				coordConfig.ServerConfig.Endpoint.Port = port
			}
			protocoordinatorservice.RegisterCoordinatorServer(server, coordService)
			coordGrpcServer = server
			wg.Done()
		})
	}()
	wg.Wait()

	m, blockGen, loadClient, err := BlockgenStarter(coordinatorLogger.Info, coordinatorBlockGenConfigFilePath)
	utils.Must(err)
	metrics = m
	go func() {
		utils.Must(loadClient.Start(blockGen))
	}()

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.blockSentTotal) > 10 &&
			test.GetMetricValue(t, metrics.transactionSentTotal) > 10 &&
			test.GetMetricValue(t, metrics.transactionReceivedTotal) > 1
	}, 10*time.Second, 100*time.Millisecond)

	close(stopSender)

	c, err := readConfig(coordinatorBlockGenConfigFilePath)
	require.NoError(t, err)

	client := &http.Client{}
	url := fmt.Sprintf("http://%s/metrics", c.Monitoring.Metrics.Endpoint.String())
	expectedMetrics := []string{
		"blockgen_block_sent_total",
		"blockgen_transaction_sent_total",
		"blockgen_transaction_received_total",
		"blockgen_valid_transaction_latency_seconds",
		"blockgen_invalid_transaction_latency_seconds",
	}
	test.CheckMetrics(t, client, url, expectedMetrics)

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) ==
			test.GetMetricValue(t, metrics.transactionReceivedTotal)
	}, 20*time.Second, 500*time.Millisecond)
}
