package main

import (
	"fmt"
	"log"
	"os"
	"time"

	shardsproto "github.ibm.com/decentralized-trust-research/scalable-committer/protos/shardsservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice"
	connectionUtils "github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/cmd/testclient/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db/goleveldb"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

func main() {

	// the db we use for the benchmark
	dbType := goleveldb.GoLevelDb
	db.Register(dbType, func(path string) (db.Database, error) {
		return goleveldb.Open(path)
	})

	// make some space for the db shards
	dir, err := os.MkdirTemp("./", "tmp_db_")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c := shardsservice.ShardServiceConfig{
		Server: &connection.ServerConfig{Endpoint: connection.Endpoint{
			Host: "localhost",
			Port: 5101,
		}},
		Database: &shardsservice.DatabaseConfig{
			Type:    dbType,
			RootDir: dir,
		},
		Limits: &shardsservice.LimitsConfig{
			MaxPhaseOneResponseBatchItemCount: 100,
			PhaseOneResponseCutTimeout:        10 * time.Millisecond,
			MaxPhaseOneProcessingWorkers:      50,
			MaxPhaseTwoProcessingWorkers:      50,
			MaxPendingCommitsBufferSize:       100,
			MaxShardInstancesBufferSize:       100,
		},
	}
	m := &metrics.Metrics{Enabled: false}

	connection.RunServerMainAndWait(c.Server, func(server *grpc.Server, port int) {
		if c.Server.Endpoint.Port == 0 {
			c.Server.Endpoint.Port = port
		}
		shardsproto.RegisterShardsServer(server, shardsservice.NewShardsCoordinator(c.Database, c.Limits, m))
	})

	client, err := connectionUtils.NewClient(connectionUtils.ClientConfig{
		Connections:        []*connection.DialConfig{connection.NewDialConfig(c.Server.Endpoint)},
		NumShardsPerServer: 4,
		Input: connectionUtils.ClientInputConfig{
			BlockCount:     100_00,
			BlockSize:      100,
			TxSize:         1,
			SignatureBytes: false,
			InputDelay:     test.Constant(int64(100 * time.Microsecond)),
		},
	})

	fmt.Printf("Start benchmark...\n")

	utils.Must(err)
	utils.Every(time.Second, client.LogDebug)
	client.Start()
	defer client.CleanUp()
	client.WaitUntilDone()
	client.LogDebug()
}
