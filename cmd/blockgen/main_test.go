package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"google.golang.org/grpc"
)

var (
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

	testConfigFilePath = "./test-config.yaml"

	blockgenConfgFilePath = "../../config/config-blockgenforcoordinator.yaml"
)

func TestBlockGenForCoordinator(t *testing.T) { // nolint: gocognit
	sigVerServerConfig, mockSigVer, sigVerGrpc := sigverifiermock.StartMockSVService(1)
	vcServerConfig, mockVC, vcGrpc := vcservicemock.StartMockVCService(1)
	var coordService *coordinatorservice.CoordinatorService
	var coordGrpcServer *grpc.Server

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

	cmd := blockgenCmd()
	cmdStdOut := new(bytes.Buffer)
	cmd.SetOut(cmdStdOut)
	cmd.SetArgs([]string{"start", "--configs", blockgenConfgFilePath, "--component", "coordinator"})

	go func() {
		err = cmd.Execute()
	}()

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.blockSentTotal) > 1 &&
			test.GetMetricValue(t, metrics.transactionSentTotal) > 1 &&
			test.GetMetricValue(t, metrics.transactionReceivedTotal) > 1
	}, 2*time.Second, 100*time.Millisecond)

	close(stopSender)

	require.Eventually(t, func() bool {
		out := cmdStdOut.String()
		return strings.Contains(out, "blockgen started") &&
			strings.Contains(out, "Start sending blocks to coordinator service") &&
			strings.Contains(out, "Start receiving status from coordinator service")
	}, 2*time.Second, 250*time.Millisecond)

	c, err := readConfig()
	require.NoError(t, err)

	client := &http.Client{}
	url := fmt.Sprintf("http://%s/metrics", c.Monitoring.Metrics.Endpoint.String())
	expectedMetrics := []string{
		"blockgen_block_sent_total",
		"blockgen_transaction_sent_total",
		"blockgen_transaction_received_total",
	}
	test.CheckMetrics(t, client, url, expectedMetrics)

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) ==
			test.GetMetricValue(t, metrics.transactionReceivedTotal)
	}, 20*time.Second, 500*time.Millisecond)
}

var configTemplateForVCService = `
logging:
  enabled: true
  level: debug
  Caller: true
  Development: true
  Output: %s
validator-committer-service:
  server:
    endpoint:
      host: localhost
      port: 9002
  database:
    host: %s
    port: %s
    username: %s
    password: %s
    database: yugabyte
    max-connections: 1000
    min-connections: 5
    load-balance: false
  resource-limits:
    max-workers-for-preparer: 2
    max-workers-for-validator: 2
    max-workers-for-committer: 2
  monitoring:
    metrics:
      endpoint: localhost:2111
`

func TestBlockGenForVCService(t *testing.T) {
	dbRunner := &runner.YugabyteDB{}
	require.NoError(t, dbRunner.Start())
	t.Cleanup(func() {
		require.NoError(t, dbRunner.Stop())
	})

	tmpDir := t.TempDir()
	output := filepath.Clean(path.Join(tmpDir, "logger-output.txt"))
	testConfigPath := filepath.Clean(path.Join(tmpDir, "test-config-vcservice.yaml"))
	conn := dbRunner.ConnectionSettings()
	configFromTmp := fmt.Sprintf(configTemplateForVCService, output, conn.Host, conn.Port, conn.User, conn.Password)
	fmt.Println(configFromTmp)
	require.NoError(t, os.WriteFile(testConfigPath, []byte(configFromTmp), 0o600))

	logCnf := &logging.Config{
		Enabled:     true,
		Level:       logging.Info,
		Caller:      false,
		Development: true,
		Output:      output,
	}
	logging.SetupWithConfig(logCnf)

	var vcGrpcServer *grpc.Server

	t.Cleanup(func() {
		require.NoError(t, os.Remove(output))
		require.NoError(t, os.Remove(testConfigPath))
		vcGrpcServer.Stop()
	})

	require.NoError(t, config.ReadYamlConfigs([]string{testConfigPath}))
	vcserviceConfig := vcservice.ReadConfig()

	require.NoError(t, vcservice.InitDatabase(vcserviceConfig.Database, []int{0}))

	vcService, err := vcservice.NewValidatorCommitterService(vcserviceConfig)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		connection.RunServerMain(vcserviceConfig.Server, func(server *grpc.Server, port int) {
			if vcserviceConfig.Server.Endpoint.Port == 0 {
				vcserviceConfig.Server.Endpoint.Port = port
			}
			protovcservice.RegisterValidationAndCommitServiceServer(server, vcService)
			vcGrpcServer = server
			wg.Done()
		})
	}()
	wg.Wait()

	cmd := blockgenCmd()
	cmdStdOut := new(bytes.Buffer)
	cmd.SetOut(cmdStdOut)
	cmd.SetArgs([]string{"start", "--configs", blockgenConfgFilePath, "--component", "vcservice"})

	go func() {
		err = cmd.Execute()
	}()

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) > 10 &&
			test.GetMetricValue(t, metrics.transactionReceivedTotal) > 10
	}, 2*time.Second, 100*time.Millisecond)

	close(stopSender)

	require.Eventually(t, func() bool {
		out := cmdStdOut.String()
		return strings.Contains(out, "blockgen started") &&
			strings.Contains(out, "Start sending transactions to vc service") &&
			strings.Contains(out, "Start receiving status from vc service")
	}, 2*time.Second, 250*time.Millisecond)

	c, err := readConfig()
	require.NoError(t, err)

	client := &http.Client{}
	url := fmt.Sprintf("http://%s/metrics", c.Monitoring.Metrics.Endpoint.String())
	expectedMetrics := []string{
		"blockgen_transaction_sent_total",
		"blockgen_transaction_received_total",
	}
	test.CheckMetrics(t, client, url, expectedMetrics)

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) ==
			test.GetMetricValue(t, metrics.transactionReceivedTotal)
	}, 20*time.Second, 500*time.Millisecond)
}
