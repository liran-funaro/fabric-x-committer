package main

import (
	"fmt"
	"sync"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/cmd/testclient/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

func main() {
	wg := sync.WaitGroup{}
	wg.Add(1)
	c := shardsservice.ReadConfig()

	go connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(server *grpc.Server) {
		shardsservice.RegisterShardsServer(server, shardsservice.NewShardsCoordinator(c.Database, c.Limits, metrics.New(c.Prometheus.Enabled)))
		wg.Done()
	})
	//monitoring.LaunchPrometheus(c.Prometheus, monitoring.ShardsService, metrics.New(c.Prometheus.Enabled).AllMetrics())
	wg.Wait()

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
			client.Debug()
		}
	}()

	result := client.WaitUntilDone()
	fmt.Printf("Execution finished: %v TPS (%v)", result.RequestsPer(time.Second), result)
}
