package main

import (
	"fmt"

	shardsproto "github.ibm.com/decentralized-trust-research/scalable-committer/protos/shardsservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db/goleveldb"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db/mockdb"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db/pebbledb"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/grpc"
)

func main() {

	// register supported dbs
	db.Register(goleveldb.GoLevelDb, func(path string) (db.Database, error) {
		return goleveldb.Open(path)
	})

	db.Register(mockdb.MockDb, func(path string) (db.Database, error) {
		return mockdb.Open(path)
	})

	db.Register(pebbledb.PebbleDb, func(path string) (db.Database, error) {
		return pebbledb.Open(path)
	})

	config.ServerConfig("shards-service")
	config.Int("max-pending-commits-size", "shards-service.limits.max-pending-commits-buffer-size", "Max size of pending-commits buffer")
	config.Int("max-shard-instance-size", "shards-service.limits.max-shard-instances-buffer-size", "Max size of shard-instances buffer")
	config.Int("max-phase-one-workers", "shards-service.limits.max-phase-one-processing-workers", "Max size of workers that consume phase-one request batches")
	config.Int("max-phase-two-workers", "shards-service.limits.max-phase-two-processing-workers", "Max size of workers that consume phase-two request batches")
	config.String("db-type", "shards-service.database.type", fmt.Sprintf("Supported DB types: %v", db.RegisteredDB()))
	config.ParseFlags()

	c := shardsservice.ReadConfig()

	m := monitoring.LaunchMonitoring(c.Monitoring, &metrics.Provider{}).(*metrics.Metrics)

	connection.RunServerMain(c.Server, func(server *grpc.Server, port int) {
		if c.Server.Endpoint.Port == 0 {
			c.Server.Endpoint.Port = port
		}
		shardsproto.RegisterShardsServer(server, shardsservice.NewShardsCoordinator(c.Database, c.Limits, m))
	})
}
