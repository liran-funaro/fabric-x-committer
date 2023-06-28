package main

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/sigverification"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
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

	m := monitoring.LaunchMonitoring(c.Monitoring, &metrics.Provider{}).(*metrics.Metrics)

	connection.RunServerMain(c.Server, func(server *grpc.Server, port int) {
		if c.Server.Endpoint.Port == 0 {
			c.Server.Endpoint.Port = port
		}
		sigverification.RegisterVerifierServer(server, verifierserver.New(&c.ParallelExecutor, c.Scheme, m))
	})
}
