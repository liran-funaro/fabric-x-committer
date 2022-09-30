package main

import (
	"fmt"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/grpc"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
)

func main() {
	config.ServerConfig("coordinator")

	config.ParseFlags()

	c := pipeline.ReadConfig()

	monitoring.LaunchPrometheus(c.Prometheus, "coordinator", metrics.AllMetrics)

	coordinator, err := pipeline.NewCoordinator(c.SigVerifiers, c.ShardsServers, c.Prometheus.Enabled)
	if err != nil {
		panic(fmt.Sprintf("Error while constructing coordinator: %s", err))
	}
	connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(server *grpc.Server) {
		coordinatorservice.RegisterCoordinatorServer(server, &serviceImpl{Coordinator: coordinator})
	})
}
