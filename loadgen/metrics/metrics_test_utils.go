package metrics

import (
	"testing"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

// Values a snapshot of the metrics values.
type Values struct {
	URL                       string
	TransactionSentTotal      float64
	TransactionReceivedTotal  float64
	TransactionCommittedTotal float64
	TransactionAbortedTotal   float64
}

// GetMetricsForTests returns a snapshot of the metrics values.
func GetMetricsForTests(t *testing.T, m *PerfMetrics) Values {
	return Values{
		URL:                       m.URL(),
		TransactionSentTotal:      test.GetMetricValue(t, m.transactionSentTotal),
		TransactionReceivedTotal:  test.GetMetricValue(t, m.transactionReceivedTotal),
		TransactionCommittedTotal: test.GetMetricValue(t, m.transactionCommittedTotal),
		TransactionAbortedTotal:   test.GetMetricValue(t, m.transactionAbortedTotal),
	}
}
