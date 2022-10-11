package main

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/grpc"
)

func main() {
	config.ServerConfig("sig-verification")

	config.Int("parallelism", "sig-verification.parallel-executor.parallelism", "Executor parallelism")
	config.Duration("batch-time-cutoff", "sig-verification.parallel-executor.batch-time-cutoff", "Batch time cutoff limit")
	config.Int("batch-size-cutoff", "sig-verification.parallel-executor.batch-size-cutoff", "Batch size cutoff limit")
	config.Int("channel-buffer-size", "sig-verification.parallel-executor.channel-buffer-size", "Channel buffer size for the executor")

	config.String("scheme", "sig-verification.scheme", "Verification scheme")

	config.ParseFlags()

	c := serverconfig.ReadConfig()
	m := metrics.New(c.Prometheus.Enabled)

	monitoring.LaunchPrometheus(c.Prometheus, monitoring.SigVerifier, m.AllMetrics())

	connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(grpcServer *grpc.Server) {
		sigverification.RegisterVerifierServer(grpcServer, verifierserver.New(&c.ParallelExecutor, c.Scheme, m))
	})
}
