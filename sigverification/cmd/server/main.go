package main

import (
	"flag"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/performance"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/utils/performance"
	"google.golang.org/grpc"
)

func main() {
	connection.ServerConfigFlags(*serverconfig.Config.Connection())

	flag.Int("parallelism", serverconfig.Config.ParallelExecutor.Parallelism, "Executor parallelism")
	flag.Duration("batch-time-cutoff", serverconfig.Config.ParallelExecutor.BatchTimeCutoff, "Batch time cutoff limit")
	flag.Int("batch-size-cutoff", serverconfig.Config.ParallelExecutor.BatchSizeCutoff, "Batch size cutoff limit")
	flag.Int("channel-buffer-size", serverconfig.Config.ParallelExecutor.ChannelBufferSize, "Channel buffer size for the executor")

	flag.String("scheme", serverconfig.Config.Scheme, "Verification scheme")

	config.ParseFlags(
		"server", "sig-verification.endpoint",
		"prometheus-enabled", "sig-verification.prometheus.enabled",
		"prometheus-endpoint", "sig-verification.prometheus.endpoint",

		"parallelism", "sig-verification.parallel-executor.parallelism",
		"batch-time-cutoff", "sig-verification.parallel-executor.batch-time-cutoff",
		"batch-size-cutoff", "sig-verification.parallel-executor.batch-size-cutoff",
		"channel-buffer-size", "sig-verification.parallel-executor.channel-buffer-size",

		"scheme", "sig-verification.scheme",
	)

	connection.RunServerMain(serverconfig.Config.Connection(), func(grpcServer *grpc.Server) {
		sigverification.RegisterVerifierServer(grpcServer, verifierserver.New(&serverconfig.Config.ParallelExecutor, serverconfig.Config.Scheme))
	})
}
