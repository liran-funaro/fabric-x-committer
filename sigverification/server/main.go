package main

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"google.golang.org/grpc"
)

var defaultConfig = &utils.ServerConfig{Endpoint: utils.Endpoint{Host: "localhost", Port: config.GRPC_PORT}}

func main() {
	utils.RunServerMain(defaultConfig, func(grpcServer *grpc.Server) {
		sigverification.RegisterVerifierServer(grpcServer, sigverification.NewVerifierServer())
	})
}
