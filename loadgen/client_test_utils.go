package loadgen

import (
	"context"
	_ "embed"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
	"google.golang.org/grpc"
)

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
func activateVcServiceForTest(t *testing.T, count int) (*yuga.Connection, []*connection.Endpoint) {
	t.Helper()
	conn := yuga.PrepareTestEnv(t)

	endpoints := make([]*connection.Endpoint, count)

	for i := range endpoints {
		vcConf := &vcservice.ValidatorCommitterServiceConfig{
			Server:     connection.NewLocalHostServer(),
			Monitoring: defaultMonitoring(),
			Database: &vcservice.DatabaseConfig{
				Endpoints:      conn.Endpoints,
				Username:       conn.User,
				Password:       conn.Password,
				Database:       conn.Database,
				MaxConnections: 10,
				MinConnections: 1,
				LoadBalance:    false,
			},
			ResourceLimits: &vcservice.ResourceLimitsConfig{
				MaxWorkersForPreparer:  2,
				MaxWorkersForCommitter: 2,
				MaxWorkersForValidator: 2,
			},
		}

		initCtx, initCancel := context.WithTimeout(t.Context(), 5*time.Minute)
		t.Cleanup(initCancel)
		service, err := vcservice.NewValidatorCommitterService(initCtx, vcConf)
		require.NoError(t, err)
		t.Cleanup(service.Close)
		test.RunServiceAndGrpcForTest(t.Context(), t, service, vcConf.Server, func(server *grpc.Server) {
			protovcservice.RegisterValidationAndCommitServiceServer(server, service)
		})
		endpoints[i] = &vcConf.Server.Endpoint
	}
	return conn, endpoints
}

// GenerateNamespacesUnderTest is an export function that creates namespaces using the vc-service.
func GenerateNamespacesUnderTest(t *testing.T, namespaces []string) *yuga.Connection {
	conn, endpoints := activateVcServiceForTest(t, 1)

	clientConf := defaultClientConf()
	clientConf.Adapter.VCClient = &adapters.VCClientConfig{
		Endpoints: endpoints,
	}
	policy := &workload.PolicyProfile{
		NamespacePolicies: make(map[string]*workload.Policy, len(namespaces)),
	}
	for i, ns := range namespaces {
		policy.NamespacePolicies[ns] = &workload.Policy{
			Scheme: signature.Ecdsa,
			Seed:   int64(i),
		}
	}
	clientConf.LoadProfile.Transaction.Policy = policy
	clientConf.Generate = &Generate{Namespaces: true}
	runLoadGenerator(t, clientConf)
	return conn
}

// defaultClientConf returns default config values for client testing.
func defaultClientConf() *ClientConfig {
	return &ClientConfig{
		LoadProfile: &workload.Profile{
			Key:   workload.KeyProfile{Size: 32},
			Block: workload.BlockProfile{Size: 500},
			Transaction: workload.TransactionProfile{
				ReadWriteCount: workload.NewConstantDistribution(2),
				Policy: &workload.PolicyProfile{
					NamespacePolicies: map[string]*workload.Policy{
						"0": {
							Scheme: signature.Ecdsa,
							Seed:   10,
						},
						"_meta": {
							Scheme: signature.Ecdsa,
							Seed:   11,
						},
					},
				},
			},
			Conflicts: workload.ConflictProfile{
				InvalidSignatures: 0.1,
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
		Generate: &Generate{
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
