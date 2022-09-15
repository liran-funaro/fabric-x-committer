package main

import (
	"flag"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"google.golang.org/grpc"
)

type ServerConfig struct {
	Connection         utils.ServerConfig
	Executor           parallelexecutor.Config
	VerificationScheme signature.Scheme
}

var serverConfig ServerConfig

func main() {
	flag.StringVar(&serverConfig.Connection.Host, "host", "localhost", "Server host to connect to")
	flag.IntVar(&serverConfig.Connection.Port, "port", config.DefaultGRPCPortSigVerifier, "Server port to connect to")
	flag.BoolVar(&serverConfig.Connection.PrometheusEnabled, "prometheus", true, "Enable prometheus metrics to be kept")

	flag.IntVar(&serverConfig.Executor.Parallelism, "parallelism", 1, "Executor parallelism")
	flag.DurationVar(&serverConfig.Executor.BatchTimeCutoff, "batch-time-cutoff", 10*time.Millisecond, "Batch time cutoff limit")
	flag.IntVar(&serverConfig.Executor.BatchSizeCutoff, "batch-size-cutoff", 100, "Batch size cutoff limit")
	flag.IntVar(&serverConfig.Executor.ChannelBufferSize, "channel-buffer-size", 2, "Channel buffer size for the executor")

	signature.SchemeVar(&serverConfig.VerificationScheme, "scheme", signature.Ecdsa, "Verification scheme")

	flag.Parse()

	utils.RunServerMain(&serverConfig.Connection, func(grpcServer *grpc.Server) {
		sigverification.RegisterVerifierServer(grpcServer, verifierserver.New(&serverConfig.Executor, serverConfig.VerificationScheme))
	})
}
