package main

import (
	"flag"
	"log"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/performance"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/utils/performance"
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
		PhaseOneResponseCutTimeout:        10 * time.Millisecond,
	},
}

func main() {

	configFileDir := flag.String("configfiledir", "", "directory where the yaml file holding the shard service configuration is placed")

	flag.Parse()

	if *configFileDir != "" {
		var err error
		conf, err = shardsservice.ReadConfig(*configFileDir)
		log.Fatal(err.Error())
	}

	sConf := &connection.ServerConfig{
		Prometheus: shardsservice.Config.Prometheus,
		Endpoint:   shardsservice.Config.Endpoint,
	}

	connection.RunServerMain(sConf, func(grpcServer *grpc.Server) {
		shardsservice.RegisterShardsServer(
			grpcServer,
			shardsservice.NewShardsCoordinator(conf),
		)

	})
}
