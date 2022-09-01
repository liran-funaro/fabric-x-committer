package main

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"google.golang.org/grpc"
	"time"
)

var defaultConfig = &utils.ServerConfig{Endpoint: utils.Endpoint{Host: "localhost", Port: config.GRPC_PORT}}
var defaultParallelExecutorConfig = &parallelexecutor.Config{
	Parallelism:       3,
	BatchTimeCutoff:   1 * time.Millisecond,
	BatchSizeCutoff:   5,
	ChannelBufferSize: 2,
}

func main() {
	utils.RunServerMain(defaultConfig, func(grpcServer *grpc.Server) {
		sigverification.RegisterVerifierServer(grpcServer, verifierserver.New(defaultParallelExecutorConfig, signature.Ecdsa))
	})
}
