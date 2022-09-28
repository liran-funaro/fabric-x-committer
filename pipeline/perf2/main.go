package main

import (
	"fmt"
	_ "net/http/pprof"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/perf/track"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

var testConfig = `
coordinator:
  sig-verifiers:
    endpoints:
      - localhost:5000
  shards-servers:
    servers:
      - endpoint: localhost:6000
        num-shards: 1
    delete-existing-shards: false
`

func main() {
	key, bQueue, pp := workload.GetBlockWorkload("../../wgclient/out/blocks")

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
