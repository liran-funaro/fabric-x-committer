package loadgen

import (
	_ "embed"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

//go:embed config_template/vc_client.yaml
var vcClientOnlyTemplate string

func TestBlockGenForVCService(t *testing.T) { //nolint:gocognit
	// Start dependencies
	ActivateVcService(t, []int{9002, 9003}, []int{10001, 10002})

	// Start client
	loadConfig(
		t,
		"client-config.yaml",
		combineClientTemplates(vcClientOnlyTemplate),
		tempFile(t, "client-log.txt"),
		2110,
		9002,
		9003,
	)
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
		return test.GetMetricValue(t, metrics.transactionSentTotal) > 100
		// return test.GetMetricValue(t, metrics.transactionSentTotal) ==
		//	test.GetMetricValue(t, metrics.transactionReceivedTotal)
	}, 20*time.Second, 500*time.Millisecond)
}
