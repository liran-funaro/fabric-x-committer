package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	_ "github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client/codec"
)

func PumpToCoordinator(path, host string, port int) {
	// read blocks from file into channel
	dQueue, pp := getWorkload(path)

	// wait quickly

	// TODO book keeping of transaction invocation start and finish
	// TODO post-processing transaction latency

	clientConfig := connection.NewDialConfig(connection.Endpoint{
		Host: host,
		Port: port,
	})

	fmt.Printf("Connect to coordinator...\n")
	conn, err := connection.Connect(clientConfig)
	utils.Must(err)

	ctx := context.Background()
	client := coordinatorservice.NewCoordinatorClient(conn)

	// we use our customCodec to the already serialized blocks from disc
	blockStream, err := client.BlockProcessing(ctx, grpc.CallContentSubtype("customCodec"))
	utils.Must(err)

	// start receive
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		fmt.Printf("Spawning response listener...")
		defer wg.Done()
		for {
			response, err := blockStream.Recv()
			if err == io.EOF {
				// end of blockStream
				fmt.Printf("EOF\n")
				break
			}

			if err != nil {
				fmt.Printf("err: %v\n", err)
				break
			}
			_ = response
		}
	}()

	// start consuming blocks
	start := time.Now()
	bar := workload.NewProgressBar("Sending blocks from file...", pp.Block.Count)
	for b := range dQueue {
		if err := blockStream.SendMsg(b); err != nil {
			utils.Must(err)
		}
		bar.Add(1)
	}
	elapsed := time.Since(start)
	printStats(pp, elapsed)

	err = blockStream.CloseSend()
	utils.Must(err)

	wg.Wait()
}

func ReadAndForget(path string) {

	dQueue, pp := getWorkload(path)

	// start consuming blocks
	start := time.Now()
	bar := workload.NewProgressBar("Reading blocks from file...", pp.Block.Count)
	for b := range dQueue {
		_ = b
		block := &token.Block{}
		err := proto.Unmarshal(b, block)
		utils.Must(err)
		_ = block

		bar.Add(1)
	}
	elapsed := time.Since(start)
	printStats(pp, elapsed)
}

func printStats(pp *workload.Profile, elapsed time.Duration) {
	fmt.Printf("\nResult: %d tx (%d blocks)  pushed in %s\n", pp.Block.Count*pp.Block.Size, pp.Block.Count, elapsed)
	fmt.Printf("tps: %f\n", float64(pp.Block.Count*pp.Block.Size)/elapsed.Seconds())
}

func getWorkload(path string) (chan []byte, *workload.Profile) {
	f, err := os.Open(path)
	utils.Must(err)
	// TODO can we improve here by tweaking the buffered reader size?
	reader := bufio.NewReader(f)

	// load profile from block file
	pp := workload.ReadProfileFromBlockFile(reader)

	// reading blocks into buffered channel
	queueSize := 1000
	dQueue := make(chan []byte, queueSize)
	go workload.ByteReader(reader, dQueue, func() {})

	return dQueue, pp
}
