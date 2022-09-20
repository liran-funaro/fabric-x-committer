package main

import (
	"fmt"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
)

func BenchmarkCoordinator(b *testing.B) {
	// go test -bench=BenchmarkCoordinator -benchmem -memprofile -blockprofile -cpuprofile profile.out
	// go tool pprof profile.out
	numTxPerBlock := 100
	serialNumPerTx := 1
	numBlocks := 50000

	numSigVerifiers := 5
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

	bg := testutil.NewBlockGenerator(numTxPerBlock, serialNumPerTx, false)
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
	fmt.Printf("time taken: %f sec. Total Status Recieved: %d \n", totalTime.Seconds(), counter)

}
