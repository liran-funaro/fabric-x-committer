package main

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/performance"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"google.golang.org/grpc"
)

func main() {
	config.String("server", "sig-verification.endpoint", "Where the server listens for incoming connections")
	config.Bool("prometheus-enabled", "sig-verification.prometheus.enabled", "Enable prometheus metrics to be kept")
	config.String("prometheus-endpoint", "sig-verification.prometheus.endpoint", "Where prometheus listens for incoming connections")

	config.Int("parallelism", "sig-verification.parallel-executor.parallelism", "Executor parallelism")
	config.Duration("batch-time-cutoff", "sig-verification.parallel-executor.batch-time-cutoff", "Batch time cutoff limit")
	config.Int("batch-size-cutoff", "sig-verification.parallel-executor.batch-size-cutoff", "Batch size cutoff limit")
	config.Int("channel-buffer-size", "sig-verification.parallel-executor.channel-buffer-size", "Channel buffer size for the executor")

	config.String("scheme", "sig-verification.scheme", "Verification scheme")

	config.ParseFlags()

	c := serverconfig.ReadConfig()

	connection.RunServerMain(c.Connection(), func(grpcServer *grpc.Server) {
		sigverification.RegisterVerifierServer(grpcServer, verifierserver.New(&c.ParallelExecutor, c.Scheme, c.Prometheus.Enabled))
	})
}
