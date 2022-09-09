package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {

	// TODO get path via args
	// refactor using cobra
	path := filepath.Join("../..", "out", "blocks")

	f, err := os.Open(path)
	utils.Must(err)
	// TODO can we improve here by tweaking the buffered reader size?
	reader := bufio.NewReader(f)

	// load profile from block file
	pp := workload.ReadProfileFromBlockFile(reader)
	blockCount := pp.Block.Count

	// reading blocks into buffered channel
	queueSize := 1000
	dQueue := make(chan []byte, queueSize)
	go workload.ByteReader(reader, dQueue, func() {})

	// TODO create grpc stream with coordinator
	// TODO start listing on coordinator return stream
	// TODO booking of transaction invocation start and finish
	// TODO post-processing transaction latency

	// start consuming blocks
	start := time.Now()
	bar := workload.NewProgressBar("Reading blocks from file...", blockCount)
	for b := range dQueue {
		// TODO send over stream to coordinator
		_ = b
		time.Sleep(10 * time.Microsecond)
		bar.Add(1)
	}
	elapsed := time.Since(start)
	fmt.Printf("\nResult: %d tx (%d blocks)  pushed in %s\n", blockCount*pp.Block.Size, blockCount, elapsed)
}
