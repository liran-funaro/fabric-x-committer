package main

import (
	"fmt"
	"testing"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/pipeline"
	"github.ibm.com/decentralized-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/pipeline/perf/track"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	workload "github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload/v1"
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
		Monitoring: monitoring.Config{},
		Server:     &connection.ServerConfig{Endpoint: connection.Endpoint{}},
		SigVerifiers: &pipeline.SigVerifierMgrConfig{
			Endpoints: []*connection.Endpoint{connection.CreateEndpoint("localhost:5000")},
		},
		ShardsServers: &pipeline.ShardsServerMgrConfig{
			Servers:                       []*pipeline.ShardServerInstanceConfig{{connection.CreateEndpoint("localhost:6000"), 1}},
			PrefixSizeForShardCalculation: 2,
			DeleteExistingShards:          false,
		},
		Limits: &pipeline.LimitsConfig{
			ShardRequestCutTimeout:       1 * time.Millisecond,
			DependencyGraphUpdateTimeout: 1 * time.Millisecond,
			MaxDependencyGraphSize:       1000000,
		},
	}

	bg := workload.NewBlockGenerator(numTxPerBlock, serialNumPerTx, false)
	defer bg.Stop()

	grpcServers, err := track.StartGrpcServers(c.SigVerifiers.Endpoints, c.ShardsServers.GetEndpoints())
	if err != nil {
		panic(fmt.Sprintf("Error in starting grpc servers: %s", err))
	}
	defer grpcServers.StopAll()

	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	coordinator, err := pipeline.NewCoordinator(c.SigVerifiers, c.ShardsServers, c.Limits, m)

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
