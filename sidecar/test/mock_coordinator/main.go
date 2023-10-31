package main

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/coordinatormock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

func main() {
	config.ParseFlags()

	// todo set address
	serverConfig := &connection.ServerConfig{Endpoint: *connection.CreateEndpoint("localhost:8812")}
	connection.RunServerMain(serverConfig, func(server *grpc.Server, port int) {
		protocoordinatorservice.RegisterCoordinatorServer(server, coordinatormock.NewMockCoordinator())
	})
}
