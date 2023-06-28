package main

import (
	"fmt"
	_ "net/http/pprof"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/pipeline"
	"github.ibm.com/decentralized-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/pipeline/perf/track"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/sigverification"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload"
)

func main() {
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

	key, bQueue, pp := workload.GetBlockWorkload("../../wgclient/out/blocks")

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

	fmt.Printf("key: %s\n", string(key))
	utils.Must(coordinator.SetSigVerificationKey(&sigverification.Key{SerializedBytes: key}))
	time.Sleep(1 * time.Second)
	fmt.Printf("start timer...\n")
	go func() {
		for block := range bQueue {
			coordinator.ProcessBlockAsync(block.Block)
		}
	}()

	track.TrackProgress(coordinator.TxStatusChan(), int(pp.Block.Count), int(pp.Block.Size))
}
