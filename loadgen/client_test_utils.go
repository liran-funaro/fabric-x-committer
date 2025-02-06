package loadgen

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
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

	//go:embed config_template/vc_server.yaml
	vcServerConfig string

	//go:embed config_template/single_vc_client.yaml
	vcSingleClientConfig string
)

func combineServerTemplates(additionalTemplate string) string {
	return loggingTemplate + additionalTemplate + serverTemplate
}

func combineClientTemplates(additionalTemplate string) string {
	return loggingTemplate + clientOnlyTemplate + additionalTemplate
}

func loadConfigString(t *testing.T, template string, params ...any) {
	conf := fmt.Sprintf(template, params...)
	require.NoError(t, config.LoadYamlConfigs(conf))
}

func tempFile(t *testing.T, filename string) string {
	return filepath.Clean(path.Join(t.TempDir(), filename))
}

func runLoadGenerator(t *testing.T, c *ClientConfig) *Client {
	client, err := NewLoadGenClient(c)
	require.NoError(t, err)

	test.RunServiceForTest(context.Background(), t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(client.Run(ctx))
	}, nil)
	eventuallyMetrics(t, client.resources.Metrics, func(m metrics.Values) bool {
		return m.TransactionSentTotal > 0 &&
			m.TransactionReceivedTotal > 0 &&
			m.TransactionCommittedTotal > 0 &&
			m.TransactionAbortedTotal == 0
	})
	return client
}

func testLoadGenerator(
	t *testing.T, c *ClientConfig, condition func(m metrics.Values) bool,
) {
	client := runLoadGenerator(t, c)

	test.CheckMetrics(t, &http.Client{}, metrics.GetMetricsForTests(t, client.resources.Metrics).URL, []string{
		"blockgen_block_sent_total",
		"blockgen_transaction_sent_total",
		"blockgen_transaction_received_total",
		"blockgen_valid_transaction_latency_seconds",
		"blockgen_invalid_transaction_latency_seconds",
	})

	eventuallyMetrics(t, client.resources.Metrics, condition)
}

func eventuallyMetrics(
	t *testing.T,
	m *metrics.PerfMetrics,
	condition func(m metrics.Values) bool,
) {
	if !assert.Eventually(t, func() bool {
		return condition(metrics.GetMetricsForTests(t, m))
	}, 2*time.Minute, time.Second) {
		t.Fatalf("Metrics target was not achieved: %+v", metrics.GetMetricsForTests(t, m))
	}
}

// activateVcServiceForTest activating the vc-service with the given ports for test purpose.
func activateVcServiceForTest(t *testing.T, ports, metricPorts []int) *yuga.Connection {
	conn := yuga.PrepareYugaTestEnv(t)

	for i := range ports {
		loadConfigString(t, combineServerTemplates(vcServerConfig),
			tempFile(t, "server-log.txt"),
			conn.Host, conn.Port, conn.User, conn.Password, conn.Database,
			metricPorts[i], ports[i])
		conf := vcservice.ReadConfig()

		initCtx, initCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		t.Cleanup(initCancel)
		service, err := vcservice.NewValidatorCommitterService(initCtx, conf)
		require.NoError(t, err)
		t.Cleanup(service.Close)
		test.RunServiceAndGrpcForTest(context.Background(), t, service, conf.Server, func(server *grpc.Server) {
			protovcservice.RegisterValidationAndCommitServiceServer(server, service)
		})
	}
	return conn
}

// GenerateNamespacesUnderTest is an export function that creates namespaces using the vc-service.
func GenerateNamespacesUnderTest(t *testing.T, namespaces []string) *yuga.Connection {
	conn := activateVcServiceForTest(t, []int{9123}, []int{10010})

	loadConfigString(
		t,
		combineClientTemplates(vcSingleClientConfig),
		tempFile(t, "client-log.txt"),
		2225,
		9123,
	)
	vcConfig := ReadConfig()
	policy := &workload.PolicyProfile{
		NamespacePolicies: make(map[string]*workload.Policy, len(namespaces)),
	}
	for i, ns := range namespaces {
		policy.NamespacePolicies[ns] = &workload.Policy{
			Scheme: signature.Ecdsa,
			Seed:   int64(i),
		}
	}
	vcConfig.LoadProfile.Transaction.Policy = policy
	vcConfig.Generate = &Generate{Namespaces: true}
	runLoadGenerator(t, vcConfig)
	return conn
}
