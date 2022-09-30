package main

import (
	"fmt"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"testing"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/perf/track"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
)

func BenchmarkCoordinator(b *testing.B) {
	// go test -bench=BenchmarkCoordinator -benchmem -memprofile -blockprofile -cpuprofile profile.out
	// go tool pprof profile.out
	const (
		numTxPerBlock  = 100
		serialNumPerTx = 1
		numBlocks      = 50000
	)
	c := &pipeline.CoordinatorConfig{
		Prometheus: connection.Prometheus{},
		Endpoint:   connection.Endpoint{},
		SigVerifiers: &pipeline.SigVerifierMgrConfig{
			Endpoints: []*connection.Endpoint{connection.CreateEndpoint("localhost:5000")},
		},
		ShardsServers: &pipeline.ShardsServerMgrConfig{
			Servers:              []*pipeline.ShardServerInstanceConfig{{connection.CreateEndpoint("localhost:6000"), 1}},
			DeleteExistingShards: false,
		},
	}

	bg := testutil.NewBlockGenerator(numTxPerBlock, serialNumPerTx, false)
	defer bg.Stop()

	grpcServers, err := track.StartGrpcServers(c.SigVerifiers.Endpoints, c.ShardsServers.GetEndpoints())
	if err != nil {
		panic(fmt.Sprintf("Error in starting grpc servers: %s", err))
	}
	defer grpcServers.StopAll()

	coordinator, err := pipeline.NewCoordinator(c.SigVerifiers, c.ShardsServers, c.Prometheus.Enabled)

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
