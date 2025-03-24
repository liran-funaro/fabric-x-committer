package loadgen

import (
	"context"
	_ "embed"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

const defaultBlockSize = 500

func runLoadGenerator(t *testing.T, c *ClientConfig) *Client {
	client, err := NewLoadGenClient(c)
	require.NoError(t, err)

	test.RunServiceForTest(t.Context(), t, func(ctx context.Context) error {
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

// DefaultClientConf returns default config values for client testing.
func DefaultClientConf() *ClientConfig {
	return &ClientConfig{
		LoadProfile: &workload.Profile{
			Key:   workload.KeyProfile{Size: 32},
			Block: workload.BlockProfile{Size: defaultBlockSize},
			Transaction: workload.TransactionProfile{
				ReadWriteCount: workload.NewConstantDistribution(2),
				Policy: &workload.PolicyProfile{
					NamespacePolicies: map[string]*workload.Policy{
						workload.GeneratedNamespaceID: {
							Scheme: signature.Ecdsa,
							Seed:   10,
						},
						types.MetaNamespaceID: {
							Scheme: signature.Ecdsa,
							Seed:   11,
						},
					},
				},
			},
			Seed: 12345,
			// We use small number of workers to reduce the CPU load during tests.
			Workers: 1,
		},
		Stream: &workload.StreamOptions{
			RateLimit: &workload.LimiterConfig{InitialLimit: 1_000},
			// We set low values for the buffer and batch to reduce the CPU load during tests.
			BuffersSize: 1,
			GenBatch:    1,
		},
		Generate: adapters.Phases{
			Config:     true,
			Namespaces: true,
			Load:       true,
		},
		Monitoring: metrics.Config{
			Config: defaultMonitoring(),
		},
	}
}

func defaultMonitoring() monitoring.Config {
	return monitoring.Config{
		Server: connection.NewLocalHostServer(),
	}
}
