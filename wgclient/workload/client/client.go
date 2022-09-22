package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"google.golang.org/protobuf/proto"

	_ "github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client/codec"
)

func PumpToCoordinator(path, host string, port int) {
	// read blocks from file into channel
	serializedKey, dQueue, pp := workload.GetByteWorkload(path)

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

	// send key
	key := &sigverification.Key{SerializedBytes: serializedKey}
	_, err = client.SetVerificationKey(ctx, key)
	utils.Must(err)

	// we use our customCodec to the already serialized blocks from disc
	//blockStream, err := client.BlockProcessing(ctx, grpc.CallContentSubtype("customCodec"))
	blockStream, err := client.BlockProcessing(ctx)
	utils.Must(err)

	// start receive
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		fmt.Printf("Spawning response listener...\n")
		defer wg.Done()
		for {
			response, err := blockStream.Recv()
			if err == io.EOF {
				// end of blockStream
				fmt.Printf("RECV EOF\n")
				break
			}

			if err != nil {
				fmt.Printf("RECV err: %v\n", err)
				break
			}
			_ = response
			//fmt.Printf("> %v\n", response)
		}
	}()

	fmt.Printf("starting now ...\n")

	// start consuming blocks
	start := time.Now()
	bar := workload.NewProgressBar("Sending blocks from file...", pp.Block.Count)
	for b := range dQueue {

		block := &token.Block{}
		err := proto.Unmarshal(b, block)
		utils.Must(err)

		if err := blockStream.SendMsg(block); err != nil {
			//if err := blockStream.SendMsg(b); err != nil {
			utils.Must(err)
		}
		bar.Add(1)
	}
	elapsed := time.Since(start)
	workload.PrintStats(pp.Block.Count*pp.Block.Size, pp.Block.Count, elapsed)

	err = blockStream.CloseSend()
	utils.Must(err)

	wg.Wait()
}

func ReadAndForget(path string) {

	_, dQueue, pp := workload.GetByteWorkload(path)

	numTx := pp.Block.Count * pp.Block.Size

	// let's check for duplicates
	// be careful - this may cause damage on your computer if numTx is massive!  :D
	stats := make(map[string]string, numTx)
	allowedDuplicates := 0
	foundDuplicates := 0

	// start consuming blocks
	start := time.Now()
	bar := workload.NewProgressBar("Reading blocks from file...", pp.Block.Count)
	for b := range dQueue {
		_ = b
		block := &token.Block{}
		err := proto.Unmarshal(b, block)
		utils.Must(err)
		_ = block

		// check if we have duplicates
		for i, tx := range block.Txs {
			for j, sn := range tx.SerialNumbers {
				if len(sn) != 32 {
					panic("len wrong")
				}

				// let's treat the sn as base64 string
				k := base64.StdEncoding.EncodeToString(sn)
				if val, exists := stats[k]; exists {
					fmt.Printf("Duplicate found:\n")
					fmt.Printf("%s exists for: %s\n", k, val)
					fmt.Printf("tx: %d, %d, %d\n\n", block.Number, i, j)
					foundDuplicates++
					if foundDuplicates > allowedDuplicates {
						panic(fmt.Sprintf("too many duplicates!!! found %d, allowed: %d", foundDuplicates, allowedDuplicates))
					}
				}
				stats[k] = fmt.Sprintf("tx-%d-%d-%d", block.Number, i, j)
			}
		}

		bar.Add(1)
	}
	elapsed := time.Since(start)
	workload.PrintStats(pp.Block.Count*pp.Block.Size, pp.Block.Count, elapsed)
}
