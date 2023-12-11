package loadgen

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
	"google.golang.org/grpc"
)

const (
	vcServerTemplate = loggingTemplate + `
validator-committer-service:` + serverTemplate + `
  database:
    host: %s
    port: %s
    username: %s
    password: %s
    database: %s
    max-connections: 10
    min-connections: 1
    load-balance: false
  resource-limits:
    max-workers-for-preparer: 2
    max-workers-for-validator: 2
    max-workers-for-committer: 2
`
	vcClientTemplate = clientTemplate + `
vc-client:
  endpoints:
    - localhost:%d
    - localhost:%d
`
)

func TestBlockGenForVCService(t *testing.T) { //nolint:gocognit
	// Start dependencies
	conn := yuga.PrepareYugaTestEnv(t)

	for i := 0; i < 2; i++ {
		// Start server under test
		loadConfig(t, fmt.Sprintf("server-config-%d.yaml", i), vcServerTemplate,
			tempFile(t, fmt.Sprintf("server-log-%d.txt", i)), 10000+i, 9002+i,
			conn.Host, conn.Port, conn.User, conn.Password, conn.Database)
		conf := vcservice.ReadConfig()

		if i == 0 {
			require.NoError(t, vcservice.InitDatabase(conf.Database, []int{0}))
		}

		service, err := vcservice.NewValidatorCommitterService(conf)
		require.NoError(t, err)

		server, _ := startServer(*conf.Server, func(server *grpc.Server) {
			protovcservice.RegisterValidationAndCommitServiceServer(server, service)
		})
		t.Cleanup(func() {
			server.Stop()
			service.Close()
		})
	}

	// Start client
	loadConfig(t, "client-config.yaml", vcClientTemplate, tempFile(t, "client-log.txt"), 2112, 9002, 9003)
	metrics := startLoadGenerator(t, ReadConfig())
	t.Logf("Load generator ended")

	// Check results
	test.CheckMetrics(t, &http.Client{}, metrics.provider.URL(), []string{
		"blockgen_transaction_sent_total",
		"blockgen_transaction_received_total",
		"blockgen_valid_transaction_latency_seconds",
		"blockgen_invalid_transaction_latency_seconds",
	})
	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, metrics.transactionSentTotal) > 0
		// return test.GetMetricValue(t, metrics.transactionSentTotal) ==
		//	test.GetMetricValue(t, metrics.transactionReceivedTotal)
	}, 20*time.Second, 500*time.Millisecond)
}
