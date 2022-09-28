package main

import (
	"fmt"
	"testing"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/perf/track"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

func BenchmarkCoordinator(b *testing.B) {
	// go test -bench=BenchmarkCoordinator -benchmem -memprofile -blockprofile -cpuprofile profile.out
	// go tool pprof profile.out
	const (
		numTxPerBlock  = 100
		serialNumPerTx = 1
		numBlocks      = 50000
		testConfig     = `
coordinator:
  sig-verifiers:
    endpoints:
      - localhost:5000
      - localhost:5001
      - localhost:5002
      - localhost:5003
      - localhost:5004
  shards-servers:
    servers:
      - endpoint: localhost:6000
        num-shards: 1
    delete-existing-shards: false
`
	)

	bg := testutil.NewBlockGenerator(numTxPerBlock, serialNumPerTx, false)
	defer bg.Stop()

	utils.Must(config.ReadYamlConfigString(testConfig))
	coordinatorConfig := pipeline.Config

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
