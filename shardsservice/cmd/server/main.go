package main

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/grpc"
)

func main() {
	config.ServerConfig("shards-service")
	config.ParseFlags()

	c := shardsservice.ReadConfig()
	m := metrics.New(c.Prometheus.Enabled)

	monitoring.LaunchPrometheus(c.Prometheus, "shards-service", m.AllMetrics())

	connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(grpcServer *grpc.Server) {
		shardsservice.RegisterShardsServer(grpcServer, shardsservice.NewShardsCoordinator(c.Database, c.Limits, m))
	})
}
