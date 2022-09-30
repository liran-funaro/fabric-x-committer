package main

import (
	"fmt"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	_ "net/http/pprof"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/perf/track"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	var c = &pipeline.CoordinatorConfig{
		Prometheus: monitoring.Prometheus{},
		Endpoint:   connection.Endpoint{},
		SigVerifiers: &pipeline.SigVerifierMgrConfig{
			Endpoints: []*connection.Endpoint{connection.CreateEndpoint("localhost:5000")},
		},
		ShardsServers: &pipeline.ShardsServerMgrConfig{
			Servers:              []*pipeline.ShardServerInstanceConfig{{connection.CreateEndpoint("localhost:6000"), 1}},
			DeleteExistingShards: false,
		},
	}

	key, bQueue, pp := workload.GetBlockWorkload("../../wgclient/out/blocks")

	track.StartProfiling()

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

	fmt.Printf("key: %s\n", string(key))
	utils.Must(coordinator.SetSigVerificationKey(&sigverification.Key{SerializedBytes: key}))
	time.Sleep(1 * time.Second)
	fmt.Printf("start timer...\n")
	go func() {
		for block := range bQueue {
			coordinator.ProcessBlockAsync(block)
		}
	}()

	track.TrackProgress(coordinator.TxStatusChan(), int(pp.Block.Count), int(pp.Block.Size))
}
