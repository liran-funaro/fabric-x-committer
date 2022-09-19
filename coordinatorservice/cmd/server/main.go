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
	c := pipeline.LoadConfigFromYaml(".")
	coordinator, err := pipeline.NewCoordinator(c)
	if err != nil {
		panic(fmt.Sprintf("Error while constructing coordinator: %s", err))
	}

	serviceImpl := serviceImpl{
		Coordinator: coordinator,
	}

	connection.RunServerMain(
		&connection.ServerConfig{
			Endpoint: connection.Endpoint{
				Host: "localhost",
				Port: config.DefaultGRPCPortCoordinatorServer,
			},
			PrometheusEnabled: false,
		},

		func(grpcServer *grpc.Server) {
			coordinatorservice.RegisterCoordinatorServer(grpcServer, &serviceImpl)
		})
}
