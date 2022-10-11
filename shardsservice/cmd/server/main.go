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
	config.Int("max-pending-commits-size", "shards-service.limits.max-pending-commits-buffer-size", "Max size of pending-commits buffer")
	config.Int("max-shard-instance-size", "shards-service.limits.max-shard-instances-buffer-size", "Max size of shard-instances buffer")
	config.ParseFlags()

	c := shardsservice.ReadConfig()
	m := metrics.New(c.Prometheus.Enabled)

	monitoring.LaunchPrometheus(c.Prometheus, monitoring.ShardsService, m.AllMetrics())

	connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(grpcServer *grpc.Server) {
		shardsservice.RegisterShardsServer(grpcServer, shardsservice.NewShardsCoordinator(c.Database, c.Limits, m))
	})
}
