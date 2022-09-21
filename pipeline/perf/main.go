package main

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	_ "net/http/pprof"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	startProfiling()
	numTxPerBlock := 100
	serialNumPerTx := 1
	numBlocks := 500000

	numSigVerifiers := 1
	numShardsServers := 1
	numShardsPerServer := 1

	sigVerifiersBeginPort := 5000
	shardsServersBeginPort := 6000

	sigVerifiersPorts := make([]int, numSigVerifiers)
	sigVerifiersAddresses := make([]string, numSigVerifiers)

	for i := 0; i < numSigVerifiers; i++ {
		port := sigVerifiersBeginPort + i
		sigVerifiersPorts[i] = port
		sigVerifiersAddresses[i] = fmt.Sprintf("localhost:%d", port)
	}

	shardsServerPorts := make([]int, numShardsServers)
	shardsServerAddressToNumShards := map[string]int{}

	for i := 0; i < numShardsServers; i++ {
		port := shardsServersBeginPort + i
		shardsServerPorts[i] = port

		address := fmt.Sprintf("localhost:%d", port)
		shardsServerAddressToNumShards[address] = numShardsPerServer
	}

	bg := testutil.NewBlockGenerator(numTxPerBlock, serialNumPerTx, true)
	defer bg.Stop()

	grpcServers, err := startGrpcServers(sigVerifiersPorts, shardsServerPorts)
	if err != nil {
		panic(fmt.Sprintf("Error in starting grpc servers: %s", err))
	}
	defer grpcServers.stopAll()

	coordinator, err := pipeline.NewCoordinator(
		&pipeline.Config{
			SigVerifierMgrConfig: &pipeline.SigVerifierMgrConfig{
				SigVerifierServers: sigVerifiersAddresses,
			},
			ShardsServerMgrConfig: &pipeline.ShardsServerMgrConfig{
				ShardsServersToNumShards: shardsServerAddressToNumShards,
			},
		},
	)

	if err != nil {
		panic(fmt.Sprintf("Error in constructing coordinator: %s", err))
	}
	defer coordinator.Stop()

	go func() {
		for i := 0; i < numBlocks; i++ {
			coordinator.ProcessBlockAsync(<-bg.OutputChan())
		}
	}()

	counter := 0
	printMark := 100000
	startTime := time.Now()
	for {
		status := <-coordinator.TxStatusChan()
		counter += len(status)

		if printMark <= counter {
			totalTime := time.Since(startTime)
			fmt.Printf("time taken: %f sec. Total Status Recieved: %d \n", totalTime.Seconds(), counter)
			printMark += 100000
		}

		if counter == numBlocks*numTxPerBlock {
			break
		}
	}

	totalTime := time.Since(startTime)
	workload.PrintStats(int64(counter), int64(numBlocks), totalTime)
}

func startGrpcServers(sigVerifiersPorts, shardsServesPort []int) (s *grpcServers, err error) {
	s = &grpcServers{}
	defer func() {
		if err != nil {
			s.stopAll()
		}
	}()

	s.sigVerifierServers, err = testutil.StartsSigVerifierGrpcServers(testutil.DefaultSigVerifierBehavior, sigVerifiersPorts)
	if err != nil {
		return
	}

	s.shardsServers, err = testutil.StartsShardsGrpcServers(testutil.DefaultPhaseOneBehavior, shardsServesPort)
	if err != nil {
		return
	}
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

func startProfiling() {
	// go tool pprof http://localhost:6060/debug/pprof/profile
	// go tool pprof http://localhost:6060/debug/pprof/heap
	// go tool pprof http://localhost:6060/debug/pprof/block

	go func() {
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			panic(fmt.Sprintf("Error while starting http server for profiling: %s", err))
		}
	}()

	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)
}
