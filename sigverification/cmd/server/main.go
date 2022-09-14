package main

import (
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
	Connection         *utils.ServerConfig
	Executor           *parallelexecutor.Config
	VerificationScheme signature.Scheme
}

var defaultServerConfig = &ServerConfig{
	Connection: &utils.ServerConfig{
		Endpoint: utils.Endpoint{
			Host: "localhost",
			Port: config.DefaultGRPCPortSigVerifier,
		},
		PrometheusEnabled: true,
	},
	Executor: &parallelexecutor.Config{
		Parallelism:       3,
		BatchTimeCutoff:   1 * time.Millisecond,
		BatchSizeCutoff:   100,
		ChannelBufferSize: 2,
	},
	VerificationScheme: signature.Ecdsa,
}

func main() {
	config := defaultServerConfig
	//TODO: Overwrite default config with flags

	utils.RunServerMain(config.Connection, func(grpcServer *grpc.Server) {
		sigverification.RegisterVerifierServer(grpcServer, verifierserver.New(config.Executor, config.VerificationScheme))
	})
}
