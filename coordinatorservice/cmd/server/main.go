package main

import (
	"fmt"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

func main() {
	connection.ServerConfigFlags(*pipeline.Config.Connection())
	config.ParseFlags(
		"server", "coordinator.endpoint",
		"prometheus-enabled", "coordinator.prometheus.enabled",
		"prometheus-endpoint", "coordinator.prometheus.endpoint",
	)

	coordinator, err := pipeline.NewCoordinator(pipeline.Config)
	if err != nil {
		panic(fmt.Sprintf("Error while constructing coordinator: %s", err))
	}
	connection.RunServerMain(pipeline.Config.Connection(), func(server *grpc.Server) {
		coordinatorservice.RegisterCoordinatorServer(server, &serviceImpl{Coordinator: coordinator})
	})
}
