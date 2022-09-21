package main

import (
	"flag"
	"log"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

var conf = &shardsservice.Configuration{
	Database: &shardsservice.DatabaseConf{
		Name:    "rocksdb",
		RootDir: "./",
	},
	Limits: &shardsservice.LimitsConf{
		MaxGoroutines:                     1000,
		MaxPhaseOneResponseBatchItemCount: 100,
		PhaseOneResponseCutTimeout:        50 * time.Millisecond,
	},
}

func main() {
	sConf := &connection.ServerConfig{
		Endpoint: connection.Endpoint{
			Host: "localhost",
			Port: config.DefaultGRPCPortShardsServer,
		},
	}

	configFileDir := flag.String("configfiledir", "", "directory where the yaml file holding the shard service configuration is placed")

	flag.Parse()

	if *configFileDir != "" {
		var err error
		conf, err = shardsservice.ReadConfig(*configFileDir)
		log.Fatal(err.Error())
	}

	connection.RunServerMain(sConf, func(grpcServer *grpc.Server) {
		shardsservice.RegisterShardsServer(
			grpcServer,
			shardsservice.NewShardsCoordinator(conf),
		)

	})
}
