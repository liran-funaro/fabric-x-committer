package main

import (
	"fmt"
	_ "net/http/pprof"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/perf/track"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

func main() {
	const (
		numTxPerBlock  = 100
		serialNumPerTx = 1
		numBlocks      = 500000
		testConfig     = `
coordinator:
  sig-verifiers:
    endpoints:
      - localhost:5000
  shards-servers:
    servers:
      - endpoint: localhost:6000
        num-shards: 1
    delete-existing-shards: false
  prometheus:
    enabled: true
    endpoint: :2113
`
	)

	bg := testutil.NewBlockGenerator(numTxPerBlock, serialNumPerTx, true)
	defer bg.Stop()

	utils.Must(config.ReadYamlConfigString(testConfig))
	coordinatorConfig := pipeline.Config
	track.StartProfiling()

	grpcServers, err := track.StartGrpcServers(coordinatorConfig.SigVerifiers.Endpoints, coordinatorConfig.ShardsServers.GetEndpoints())
	if err != nil {
		panic(fmt.Sprintf("Error in starting grpc servers: %s", err))
	}
	defer grpcServers.StopAll()

	coordinator, err := pipeline.NewCoordinator(coordinatorConfig)
	if err != nil {
		panic(fmt.Sprintf("Error in constructing coordinator: %s", err))
	}
	defer coordinator.Stop()

	go func() {
		for i := 0; i < numBlocks; i++ {
			coordinator.ProcessBlockAsync(<-bg.OutputChan())
		}
	}()

	track.TrackProgress(coordinator.TxStatusChan(), numBlocks, numTxPerBlock)
}
