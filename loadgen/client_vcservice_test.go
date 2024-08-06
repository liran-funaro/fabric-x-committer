package loadgen

import (
	"context"
	_ "embed"
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

//go:embed config_template/vc_server.yaml
var vcServerOnlyTemplate string

//go:embed config_template/vc_client.yaml
var vcClientOnlyTemplate string

var (
	vcServerTemplate = loggingTemplate + vcServerOnlyTemplate + serverTemplate
	vcClientTemplate = loggingTemplate + clientOnlyTemplate + vcClientOnlyTemplate
)

func TestBlockGenForVCService(t *testing.T) { //nolint:gocognit
	// Start dependencies
	conn := yuga.PrepareYugaTestEnv(t)

	for i := 0; i < 2; i++ {
		// Start server under test
		loadConfig(t, fmt.Sprintf("server-config-%d.yaml", i), vcServerTemplate,
			tempFile(t, fmt.Sprintf("server-log-%d.txt", i)),
			conn.Host, conn.Port, conn.User, conn.Password, conn.Database,
			10000+i, 9002+i)
		conf := vcservice.ReadConfig()

		if i == 0 {
			require.NoError(t, vcservice.InitDatabase(conf.Database, []int{0}))
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		t.Cleanup(cancel)
		service, err := vcservice.NewValidatorCommitterService(ctx, conf)
		require.NoError(t, err)
		t.Cleanup(func() { service.Close() })

		server, _ := startServer(*conf.Server, func(server *grpc.Server) {
			protovcservice.RegisterValidationAndCommitServiceServer(server, service)
		})
		t.Cleanup(server.Stop)
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
