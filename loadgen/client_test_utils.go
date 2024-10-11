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
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
	"google.golang.org/grpc"
)

// general config templates.
var (
	//go:embed config_template/logging.yaml
	loggingTemplate string

	//go:embed config_template/client.yaml
	clientOnlyTemplate string

	//go:embed config_template/server.yaml
	serverTemplate string
)

// vcServer and vcClient config templates.
var (
	//go:embed config_template/vc_server.yaml
	vcServerConfig string

	//go:embed config_template/vc_client.yaml
	vcClientConfig string
)

func combineServerTemplates(additionalTemplate string) string {
	return loggingTemplate + additionalTemplate + serverTemplate
}

func combineClientTemplates(additionalTemplate string) string {
	return loggingTemplate + clientOnlyTemplate + additionalTemplate
}

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
	loadBundle, err := Starter(c)
	require.NoError(t, err)
	metrics, blockGen, namespaceGen, client := loadBundle.Metrics,
		loadBundle.BlkStream,
		loadBundle.NamespaceGen,
		loadBundle.Client

	require.NoError(t, client.Start(blockGen, namespaceGen))
	go func() {
		<-client.Context().Done()
		require.ErrorIs(t, context.Cause(client.Context()), ErrStoppedByUser)
	}()
	t.Cleanup(client.Stop)

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) > 0 &&
			test.GetMetricValue(t, metrics.transactionReceivedTotal) > 0 &&
			test.GetMetricValue(t, metrics.transactionCommittedTotal) > 0 &&
			test.GetMetricValue(t, metrics.transactionAbortedTotal) == 0
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

// ActivateVcService activating the vc-service with the given ports for test purpose.
func ActivateVcService(t *testing.T, ports, metricPorts []int) *yuga.Connection {
	conn := yuga.PrepareYugaTestEnv(t)

	for i := range ports {
		loadConfig(t, "server-config.yaml", combineServerTemplates(vcServerConfig),
			tempFile(t, "server-log.txt"),
			conn.Host, conn.Port, conn.User, conn.Password, conn.Database,
			metricPorts[i], ports[i])
		conf := vcservice.ReadConfig()

		if i == 0 {
			require.NoError(t, vcservice.InitDatabase(conf.Database))
		}

		service, err := vcservice.NewValidatorCommitterService(conf)
		require.NoError(t, err)
		t.Cleanup(func() { service.Close() })

		server, _ := startServer(*conf.Server, func(server *grpc.Server) {
			protovcservice.RegisterValidationAndCommitServiceServer(server, service)
		})
		t.Cleanup(server.Stop)

		wg := sync.WaitGroup{}
		t.Cleanup(wg.Wait)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		t.Cleanup(cancel)

		wg.Add(1)
		go func() { require.NoError(t, service.Run(ctx)); wg.Done() }()
	}
	return conn
}

// GenerateNamespacesUnderTest is an export function that creates namespaces using the vc-service.
func GenerateNamespacesUnderTest(t *testing.T, namespaces []types.NamespaceID) *yuga.Connection {
	conn := ActivateVcService(t, []int{9123}, []int{10010})

	loadConfig(
		t,
		"client-config.yaml",
		combineClientTemplates(vcClientConfig),
		tempFile(t, "client-log.txt"),
		2225,
		9123,
		9123,
	)

	vcConfig := ReadConfig()
	vcConfig.LoadProfile.Transaction.Signature.Namespaces = namespaces

	loadBundle, err := Starter(vcConfig)
	require.NotNil(t, loadBundle.Client)
	t.Cleanup(loadBundle.Client.Stop)
	require.NoError(t, err)
	require.NoError(
		t,
		loadBundle.Client.startNamespaceGeneration(loadBundle.NamespaceGen),
	)

	return conn
}
