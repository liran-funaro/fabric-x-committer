package main

import (
	"fmt"
	_ "net/http/pprof"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/perf/track"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

func main() {
	const (
		numTxPerBlock  = 100
		serialNumPerTx = 1
		numBlocks      = 500000
	)

	var c = &pipeline.CoordinatorConfig{
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

	bg := testutil.NewBlockGenerator(numTxPerBlock, serialNumPerTx, true)
	defer bg.Stop()

	track.StartProfiling()

	grpcServers, err := track.StartGrpcServers(c.SigVerifiers.Endpoints, c.ShardsServers.GetEndpoints())
	if err != nil {
		panic(fmt.Sprintf("Error in starting grpc servers: %s", err))
	}
	defer grpcServers.StopAll()

	m := monitoring.LaunchMonitoring(c.Monitoring, &metrics.Provider{}).(*metrics.Metrics)
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
