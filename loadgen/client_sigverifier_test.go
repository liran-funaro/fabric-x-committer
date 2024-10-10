package loadgen

import (
	_ "embed"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

var (
	//go:embed config_template/sig_server.yaml
	sigVerifierServerOnlyTemplate string

	//go:embed config_template/sig_client.yaml
	sigVerifierClientOnlyTemplate string

	sigVerifierServerTemplate = combineServerTemplates(sigVerifierServerOnlyTemplate)
	sigVerifierClientTemplate = combineClientTemplates(sigVerifierClientOnlyTemplate)
)

func TestBlockGenForSigVerifier(t *testing.T) { // nolint: gocognit
	for i := 0; i < 2; i++ {
		// Start server under test
		loadConfig(
			t,
			fmt.Sprintf("server-config-%d.yaml", i),
			sigVerifierServerTemplate,
			tempFile(t, fmt.Sprintf("server-log-%d.yaml", i)),
			2110+i,
			5000+i,
		)
		conf := serverconfig.ReadConfig()

		service := verifierserver.New(&conf.ParallelExecutor, &metrics.Metrics{Enabled: false})

		server, _ := startServer(*conf.Server, func(server *grpc.Server) {
			protosigverifierservice.RegisterVerifierServer(server, service)
		})
		t.Cleanup(server.Stop)
	}

	// Start client
	loadConfig(t, "client-config.yaml", sigVerifierClientTemplate, tempFile(t, "client-log.txt"), 2112, 5000, 5001)
	m := startLoadGenerator(t, ReadConfig())

	// Check results
	test.CheckMetrics(t, &http.Client{}, m.provider.URL(), []string{
		"blockgen_block_sent_total",
		"blockgen_transaction_sent_total",
		"blockgen_transaction_received_total",
		"blockgen_valid_transaction_latency_seconds",
		"blockgen_invalid_transaction_latency_seconds",
	})
	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, m.transactionSentTotal) > 0
		// return test.GetMetricValue(t, metrics.transactionSentTotal) ==
		//	test.GetMetricValue(t, metrics.transactionReceivedTotal)
	}, 20*time.Second, 500*time.Millisecond)
}
