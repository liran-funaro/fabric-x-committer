package main

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"google.golang.org/grpc"
)

func main() {
	config.ServerConfig("shards-service")
	config.ParseFlags()

	c := shardsservice.ReadConfig()

	connection.RunServerMain(c.Connection(), func(grpcServer *grpc.Server) {
		shardsservice.RegisterShardsServer(grpcServer, shardsservice.NewShardsCoordinator(c.Database, c.Limits, c.Prometheus.Enabled))
	})
}
