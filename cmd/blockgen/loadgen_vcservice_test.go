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
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"google.golang.org/grpc"
)

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
      port: %d
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
      endpoint: localhost:%d
`

func TestBlockGenForVCService(t *testing.T) { //nolint:gocognit
	dbRunner := &runner.YugabyteDB{}
	require.NoError(t, dbRunner.Start())
	t.Cleanup(func() {
		require.NoError(t, dbRunner.Stop())
	})

	tmpDir := t.TempDir()

	output := filepath.Clean(path.Join(tmpDir, "logger-output.txt"))

	configFilesPath := make([]string, 0, 2)

	for _, testConfigPath := range []string{"test-config-vcservice-1.yaml", "test-config-vcservice-2.yaml"} {
		configFilesPath = append(configFilesPath, filepath.Clean(path.Join(tmpDir, testConfigPath)))
	}

	conn := dbRunner.ConnectionSettings()
	port := []int{9002, 9003}
	for i, testConfigPath := range configFilesPath {
		configFromTmp := fmt.Sprintf(
			configTemplateForVCService,
			output,
			port[i],
			conn.Host,
			conn.Port,
			conn.User,
			conn.Password,
			10000+i,
		)
		require.NoError(t, os.WriteFile(testConfigPath, []byte(configFromTmp), 0o600))
	}

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
		for _, testConfigPath := range configFilesPath {
			require.NoError(t, os.Remove(testConfigPath))
		}
		vcGrpcServer.Stop()
	})

	for i, testConfigPath := range configFilesPath {
		require.NoError(t, config.ReadYamlConfigs([]string{testConfigPath}))
		vcserviceConfig := vcservice.ReadConfig()

		if i == 0 {
			require.NoError(t, vcservice.InitDatabase(vcserviceConfig.Database, []int{0}))
		}
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
	}

	cmd := blockgenCmd()
	cmdStdOut := new(bytes.Buffer)
	cmd.SetOut(cmdStdOut)
	cmd.SetArgs([]string{"start", "--configs", blockgenConfgFilePath, "--component", "vcservice"})

	go func() {
		err := cmd.Execute()
		if err != nil {
			panic(err)
		}
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
		"blockgen_transaction_latency_seconds",
	}
	test.CheckMetrics(t, client, url, expectedMetrics)

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) ==
			test.GetMetricValue(t, metrics.transactionReceivedTotal)
	}, 20*time.Second, 500*time.Millisecond)
}
