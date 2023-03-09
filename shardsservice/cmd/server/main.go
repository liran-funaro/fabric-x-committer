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
	config.Int("max-phase-one-workers", "shards-service.limits.max-phase-one-processing-workers", "Max size of workers that consume phase-one request batches")
	config.Int("max-phase-two-workers", "shards-service.limits.max-phase-two-processing-workers", "Max size of workers that consume phase-two request batches")
	config.String("db-type", "shards-service.database.type", "DB type: 'RocksDb', 'MockDb', 'GoLevelDb'")
	config.ParseFlags()

	c := shardsservice.ReadConfig()

	m := monitoring.LaunchMonitoring(c.Monitoring, monitoring.ShardsService, &metrics.Provider{}).(*metrics.Metrics)

	connection.RunServerMain(c.Server, func(grpcServer *grpc.Server) {
		shardsservice.RegisterShardsServer(grpcServer, shardsservice.NewShardsCoordinator(c.Database, c.Limits, m))
	})
}
