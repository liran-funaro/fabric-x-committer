package main

import (
	"fmt"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"google.golang.org/protobuf/proto"
)

func main() {
	key, bQueue, pp := workload.GetWorkload("../../wgclient/out/blocks")

	grpcServers, err := startGrpcServers()
	if err != nil {
		panic(fmt.Sprintf("Error in starting grpc servers: %s", err))
	}
	defer grpcServers.stopAll()

	coordinator, err := pipeline.NewCoordinator(
		&pipeline.Config{
			SigVerifierMgrConfig: &pipeline.SigVerifierMgrConfig{
				SigVerifierServers: []string{"localhost"},
			},
			ShardsServerMgrConfig: &pipeline.ShardsServerMgrConfig{
				ShardsServersToNumShards: map[string]int{"localhost": 1},
			},
		},
	)
	if err != nil {
		panic(fmt.Sprintf("Error in constructing coordinator: %s", err))
	}
	defer coordinator.Stop()

	fmt.Printf("key: %s\n", string(key))
	err = coordinator.SetSigVerificationKey(&sigverification.Key{SerializedBytes: key})
	utils.Must(err)

	go func() {
		for i := int64(0); i < pp.Block.Count; i++ {

			block := &token.Block{}
			err := proto.Unmarshal(<-bQueue, block)
			utils.Must(err)

			coordinator.ProcessBlockAsync(block)
		}
	}()

	counter := int64(0)
	printMark := int64(100000)
	startTime := time.Now()
	for {
		status := <-coordinator.TxStatusChan()
		counter += int64(len(status))

		if printMark <= counter {
			totalTime := time.Since(startTime)
			fmt.Printf("time taken: %f sec. Total Status Recieved: %d \n", totalTime.Seconds(), counter)
			printMark += 100000
		}

		if counter == pp.Block.Count*pp.Block.Size {
			break
		}
	}

	totalTime := time.Since(startTime)
	fmt.Printf("time taken: %f sec. Total Status Recieved: %d \n", totalTime.Seconds(), counter)
}

func startGrpcServers() (s *grpcServers, err error) {
	s = &grpcServers{}
	defer func() {
		if err != nil {
			s.stopAll()
		}
	}()

	sigVerifierServer, err := testutil.NewSigVerifierGrpcServer(testutil.DefaultSigVerifierBehavior, config.DefaultGRPCPortSigVerifier)
	if err != nil {
		return nil, err
	}
	s.sigVerifierServers = append(s.sigVerifierServers, sigVerifierServer)

	shardsServer, err := testutil.NewShardsGrpcServer(testutil.DefaultPhaseOneBehavior, config.DefaultGRPCPortShardsServer)
	if err != nil {
		return nil, err
	}
	s.shardsServers = append(s.shardsServers, shardsServer)
	return
}

type grpcServers struct {
	sigVerifierServers []*testutil.SigVerifierGrpcServer
	shardsServers      []*testutil.ShardsGrpcServer
}

func (s *grpcServers) stopAll() {
	for _, s := range s.sigVerifierServers {
		s.Stop()
	}
	for _, s := range s.shardsServers {
		s.Stop()
	}
}
