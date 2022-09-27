package main

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/performance"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/utils/performance"
	"google.golang.org/grpc"
)

func main() {
	connection.ServerConfigFlags(*shardsservice.Config.Connection())
	config.ParseFlags(
		"server", "shards-service.endpoint",
		"prometheus-enabled", "shards-service.prometheus.enabled",
		"prometheus-endpoint", "shards-service.prometheus.endpoint",
	)

	connection.RunServerMain(shardsservice.Config.Connection(), func(grpcServer *grpc.Server) {
		shardsservice.RegisterShardsServer(grpcServer, shardsservice.NewShardsCoordinator(shardsservice.Config.ShardCoordinator()))
	})
}
