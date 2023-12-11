package loadgen

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

const (
	loggingTemplate = `
logging:
  enabled: true
  level: info
  caller: true
  development: true
  output: %s
`
	clientTemplate = loggingTemplate + `
monitoring:
  metrics:
    enable: true
    endpoint: localhost:%d
    latency:
      sampler:
        type: timer
        sampling-interval: 10s
      buckets:
        type: uniform
        max-latency: 5s
        bucket-count: 1000
load-profile:
  block:
    size: 500
    buffer-size: 1000
  transaction:
    key-size: 32
    read-write-count:
      type: constant
      const: 2
    buffer-size: 500
    signature:
      scheme: ECDSA
  conflicts:
    invalid-signatures: 0.1
  tx-gen-workers: 5
  tx-sign-workers: 3
  tx-dependencies-workers: 2
  seed: 12345
rate-limit:
  initial-limit: 1000
`
	serverTemplate = `
  monitoring:
    metrics:
      enable: true
      endpoint: localhost:%d
      latency:
        sampler:
          type: timer
          sampling-interval: 10s
        buckets:
          type: uniform
          max-latency: 5s
          bucket-count: 1000
  server:
    endpoint: localhost:%d
`
)

func loadConfig(t *testing.T, fileName, template string, params ...any) {
	conf := fmt.Sprintf(template, params...)
	testConfigPath := tempFile(t, fileName)
	require.NoError(t, os.WriteFile(testConfigPath, []byte(conf), 0o600))
	require.NoError(t, config.ReadYamlConfigs([]string{testConfigPath}))
}

func tempFile(t *testing.T, filename string) string {
	output := filepath.Clean(path.Join(t.TempDir(), filename))
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(output))
	})
	return output
}

func startLoadGenerator(t *testing.T, c *ClientConfig) *perfMetrics {
	logger.Debugf("Starting load generator with config: %v", c)
	metrics, blockGen, client, err := Starter(c)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, metrics.provider.StopServer())
	})
	go func() {
		require.NoError(t, client.Start(blockGen))
	}()

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) > 0 &&
			test.GetMetricValue(t, metrics.transactionReceivedTotal) > 0
	}, 20*time.Second, 100*time.Millisecond)

	client.Stop()
	return metrics
}

func startServer(conf connection.ServerConfig, register func(server *grpc.Server)) (*grpc.Server, connection.Endpoint) {
	var grpcServer *grpc.Server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		connection.RunServerMain(&conf, func(server *grpc.Server, port int) {
			if conf.Endpoint.Port == 0 {
				conf.Endpoint.Port = port
			}
			register(server)
			grpcServer = server
			wg.Done()
		})
	}()
	wg.Wait()

	return grpcServer, conf.Endpoint
}
