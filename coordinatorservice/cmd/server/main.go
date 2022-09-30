package main

import (
	"fmt"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"google.golang.org/grpc"
)

func main() {
	config.ServerConfig("coordinator")

	config.ParseFlags()

	c := pipeline.ReadConfig()

	coordinator, err := pipeline.NewCoordinator(c.SigVerifiers, c.ShardsServers, c.Prometheus.Enabled)
	if err != nil {
		panic(fmt.Sprintf("Error while constructing coordinator: %s", err))
	}
	connection.RunServerMain(c.Connection(), func(server *grpc.Server) {
		coordinatorservice.RegisterCoordinatorServer(server, &serviceImpl{Coordinator: coordinator})
	})
}
