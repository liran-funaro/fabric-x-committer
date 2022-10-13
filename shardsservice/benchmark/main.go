package main

import (
	"fmt"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/cmd/testclient/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

func main() {
	config.ParseFlags()
	c := shardsservice.ReadConfig()

	connection.RunServerMainAndWait(&connection.ServerConfig{Endpoint: c.Endpoint}, func(server *grpc.Server) {
		shardsservice.RegisterShardsServer(server, shardsservice.NewShardsCoordinator(c.Database, c.Limits, metrics.New(c.Prometheus.Enabled)))
	})
	//monitoring.LaunchPrometheus(c.Prometheus, monitoring.ShardsService, metrics.New(c.Prometheus.Enabled).AllMetrics())

	client, err := utils.NewClient(utils.ClientConfig{
		Connections:        []*connection.DialConfig{connection.NewDialConfig(c.Endpoint)},
		NumShardsPerServer: 4,
		Input: utils.ClientInputConfig{
			BlockCount:     100_000,
			BlockSize:      100,
			TxSize:         1,
			SignatureBytes: false,
			InputDelay:     test.Constant(int64(100 * time.Microsecond)),
		},
	})
	if err != nil {
		panic(err)
	}
	client.Start()
	defer client.CleanUp()

	go func() {
		for {
			<-time.After(1 * time.Second)
			client.LogDebug()
		}
	}()

	client.WaitUntilDone()
	fmt.Printf("Finished execution.")
	client.LogDebug()
}
