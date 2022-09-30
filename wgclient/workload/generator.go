package workload

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

func Generate(profilePath, outputPath string) {

	pp := LoadProfileFromYaml(profilePath)

	publicKey, txQueue := startTxGenerator(pp)
	outputPath = createIfNotExists(outputPath)

	// store public key
	savePublicKey(outputPath, publicKey)

	// create output file for blocks
	outFile, err := os.Create(outputPath)
	utils.Must(err)
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	// write profile first as our header
	WriteProfileToBlockFile(writer, pp)

	blockCount := pp.Block.Count
	blockSize := pp.Block.Size
	parallel := 2
	numWorker := runtime.NumCPU() * parallel

	// start file writer
	var wg sync.WaitGroup
	queueBufferSize := numWorker * 4
	bQueue := make(chan *token.Block, queueBufferSize)
	wg.Add(1)
	go func() {
		defer wg.Done()
		bar := NewProgressBar("Writing blocks to file...", blockCount)
		BlockWriter(writer, bQueue, func() {
			bar.Add(1)
		})
	}()

	for blockNo := int64(1); blockNo <= blockCount; blockNo++ {
		bQueue <- createBlock(blockNo, blockSize, txQueue)
	}
	close(bQueue)
	wg.Wait()
}

func createBlock(blockNo, numTx int64, txQueue chan *token.Tx) *token.Block {
	// create block
	block := &token.Block{
		Number: uint64(blockNo),
		Txs:    make([]*token.Tx, numTx),
	}

	// collect token transactions
	for i := int64(0); i < numTx; i++ {
		block.Txs[i] = <-txQueue
	}

	return block
}

func savePublicKey(outputPath string, publicKey []byte) {
	// write verification key for
	pkFilePath := outputPath + ".pem"
	fmt.Printf("Public key file: %s\n", pkFilePath)
	fmt.Printf("Writing public key to file...")
	pkFile, err := os.Create(pkFilePath)
	utils.Must(err)
	_, err = pkFile.Write(publicKey)
	utils.Must(err)
	err = pkFile.Close()
	utils.Must(err)
	fmt.Println(" 100%")

	fmt.Printf("Block output file: %s\n", outputPath)
}

func createIfNotExists(outputPath string) string {
	outputPath, err := filepath.Abs(outputPath)
	utils.Must(err)

	// check if output folder exists; otherwise try to create it
	d := filepath.Dir(outputPath)
	if _, err := os.Stat(d); os.IsNotExist(err) {
		err := os.MkdirAll(d, os.ModePerm)
		utils.Must(err)
	}

	return outputPath
}
