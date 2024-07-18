package loadgen

import (
	"context"
	_ "embed"
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

//go:embed config_template/logging.yaml
var loggingTemplate string

//go:embed config_template/client.yaml
var clientOnlyTemplate string

//go:embed config_template/server.yaml
var serverTemplate string

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

func startLoadGenerator(t *testing.T, c *ClientConfig) *PerfMetrics {
	logger.Debugf("Starting load generator with config: %v", c)
	metrics, blockGen, client, err := Starter(c)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, metrics.provider.StopServer())
	})
	require.NoError(t, client.Start(blockGen))
	go func() {
		<-client.Context().Done()
		require.ErrorIs(t, context.Cause(client.Context()), ErrStoppedByUser)
	}()
	t.Cleanup(client.Stop)

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) > 0 &&
			test.GetMetricValue(t, metrics.transactionReceivedTotal) > 0
	}, 20*time.Second, 100*time.Millisecond)

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
