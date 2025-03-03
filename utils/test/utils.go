package test

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	promgo "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type (
	// GrpcServers holds the server instances and their respective configurations.
	GrpcServers struct {
		Servers []*grpc.Server
		Configs []*connection.ServerConfig
	}

	// TestingT allows supporting both Testing and Benchmarking.
	TestingT interface {
		Errorf(format string, args ...interface{})
		FailNow()
		Cleanup(f func())
		Context() context.Context
		Helper()
	}
)

// FailHandler registers a [gomega] fail handler.
func FailHandler(t TestingT) {
	t.Helper()
	gomega.RegisterFailHandler(func(message string, _ ...int) {
		t.Helper()
		t.Errorf(message)
		t.FailNow()
	})
}

// CheckMetrics checks the metrics endpoint for the expected metrics.
func CheckMetrics(t TestingT, client *http.Client, url string, expectedMetrics []string) {
	t.Helper()
	resp, err := client.Get(url)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	bys, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	metricsOutput := string(bys)

	for _, expected := range expectedMetrics {
		require.Contains(t, metricsOutput, expected)
	}
}

// GetMetricValue returns the value of a prometheus metric.
func GetMetricValue(t TestingT, m prometheus.Metric) float64 {
	t.Helper()
	gm := promgo.Metric{}
	require.NoError(t, m.Write(&gm))

	switch {
	case gm.Gauge != nil:
		return gm.Gauge.GetValue()
	case gm.Counter != nil:
		return gm.Counter.GetValue()
	case gm.Untyped != nil:
		return gm.Untyped.GetValue()
	case gm.Summary != nil:
		return gm.Summary.GetSampleSum()
	case gm.Histogram != nil:
		return gm.Histogram.GetSampleSum()
	default:
		require.Fail(t, "unsupported metric")
		return 0
	}
}

// RunGrpcServerForTest starts a GRPC server using a register method.
// It handles the cleanup of the GRPC server at the end of a test, and ensure the test is ended
// only when the GRPC server is down.
// It also updates the server config endpoint port to the actual port if the configuration
// did not specify a port.
// The method asserts that the GRPC server did not end with failure.
func RunGrpcServerForTest(
	ctx context.Context, t TestingT, serverConfig *connection.ServerConfig, register func(server *grpc.Server),
) *grpc.Server {
	t.Helper()
	server, listener, err := connection.NewGrpcServer(serverConfig)
	require.NoError(t, err)

	serverConfig.Endpoint.Port = listener.Addr().(*net.TCPAddr).Port
	register(server)

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	t.Cleanup(server.Stop)
	wg.Add(1)
	go func() {
		defer wg.Done()
		// We use assert to prevent panicking for cleanup errors.
		assert.NoError(t, server.Serve(listener))
	}()

	_ = context.AfterFunc(ctx, func() {
		server.Stop()
	})
	return server
}

// StartGrpcServersForTest starts multiple GRPC servers with a default configuration.
func StartGrpcServersForTest(
	ctx context.Context,
	t TestingT,
	numService int,
	register func(*grpc.Server, int),
) *GrpcServers {
	t.Helper()
	sc := make([]*connection.ServerConfig, numService)
	for i := range sc {
		sc[i] = connection.NewLocalHostServer()
	}
	return StartGrpcServersWithConfigForTest(ctx, t, sc, register)
}

// StartGrpcServersWithConfigForTest starts multiple GRPC servers with given configurations.
func StartGrpcServersWithConfigForTest(
	ctx context.Context, t TestingT, sc []*connection.ServerConfig, register func(*grpc.Server, int),
) *GrpcServers {
	grpcServers := make([]*grpc.Server, len(sc))
	for i, s := range sc {
		grpcServers[i] = RunGrpcServerForTest(ctx, t, s, func(server *grpc.Server) {
			register(server, i)
		})
	}
	return &GrpcServers{
		Servers: grpcServers,
		Configs: sc,
	}
}

// RunServiceForTest runs a service using the given service method, and waits for it to be ready
// given the waitFunc method.
// It handles the cleanup of the service at the end of a test, and ensure the test is ended
// only when the service return.
// The method asserts that the service did not end with failure.
func RunServiceForTest(
	ctx context.Context,
	t TestingT,
	service func(ctx context.Context) error,
	waitFunc func(ctx context.Context) bool,
) {
	t.Helper()
	var wg sync.WaitGroup
	// NOTE: we should cancel the context before waiting for the completion. Therefore, the
	//       order of cleanup matters, which is last added first called.
	t.Cleanup(wg.Wait)
	dCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	wg.Add(1)
	go func() {
		defer wg.Done()
		// We use assert to prevent panicking for cleanup errors.
		assert.NoError(t, service(dCtx))
	}()

	if waitFunc == nil {
		return
	}

	initCtx, initCancel := context.WithTimeout(dCtx, 2*time.Minute)
	t.Cleanup(initCancel)
	require.True(t, waitFunc(initCtx))
}

// RunServiceAndGrpcForTest combines running a service and its GRPC server.
// It is intended for services that implements the Service API (i.e., command line services).
func RunServiceAndGrpcForTest(
	ctx context.Context,
	t TestingT,
	service connection.Service,
	serverConfig *connection.ServerConfig,
	register func(server *grpc.Server),
) *grpc.Server {
	t.Helper()
	RunServiceForTest(ctx, t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(service.Run(ctx))
	}, service.WaitForReady)
	return RunGrpcServerForTest(ctx, t, serverConfig, register)
}

// StatusRetriever provides implementation retrieve status of given transaction identifiers.
type StatusRetriever interface {
	GetTransactionsStatus(
		context.Context,
		*protoblocktx.QueryStatus,
		...grpc.CallOption,
	) (*protoblocktx.TransactionsStatus, error)
}

func EnsurePersistedTxStatus( // nolint:revive
	ctx context.Context,
	t TestingT,
	r StatusRetriever,
	txIDs []string,
	expected map[string]*protoblocktx.StatusWithHeight,
) {
	t.Helper()
	actualStatus, err := r.GetTransactionsStatus(ctx, &protoblocktx.QueryStatus{TxIDs: txIDs})
	require.NoError(t, err)
	require.Equal(t, expected, actualStatus.Status)
}

// CheckServerStopped returns true if the grpc server listening on a
// given address has been stopped.
func CheckServerStopped(_ *testing.T, addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.DialContext( // nolint:staticcheck
		ctx,
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(), // nolint:staticcheck
	)
	if err != nil {
		return true
	}
	_ = conn.Close()
	return false
}
