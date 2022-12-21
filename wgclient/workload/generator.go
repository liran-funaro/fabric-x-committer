package workload

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

const snFormatter = "%s-%d"

func Generate(profilePath, outputPath string) {

	pp := LoadProfileFromYaml(profilePath)

	publicKey, signer, txQueue := StartTxGenerator(&pp.Transaction, pp.Block.Size)
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

	// prepare ConflictHandler
	conflicts := NewConflictHandler(pp.Conflicts.Scenario, pp.Conflicts.Statistical, signer.SignTx)

	blockCount := pp.Block.Count
	blockSize := pp.Block.Size
	parallel := 2
	numWorker := runtime.NumCPU() * parallel

	// start file writer
	var wg sync.WaitGroup
	queueBufferSize := numWorker * 4
	bQueue := make(chan *BlockWithExpectedResult, queueBufferSize)
	wg.Add(1)
	go func() {
		defer wg.Done()
		bar := NewProgressBar("Writing blocks to file...", blockCount, "blocks")
		BlockWriter(writer, bQueue, func() {
			bar.Add(1)
		})
	}()

	for blockNo := int64(1); blockNo <= blockCount; blockNo++ {
		bQueue <- createBlock(uint64(blockNo), uint64(blockSize), txQueue, conflicts)
	}
	close(bQueue)
	wg.Wait()
}

func createBlock(blockNo, numTx uint64, txQueue chan *token.Tx, conflicts ConflictHandler) *BlockWithExpectedResult {

	b := &BlockWithExpectedResult{
		Block: &token.Block{
			Number: blockNo,
			Txs:    make([]*token.Tx, numTx),
		},
		ExpectedResults: make([]*coordinatorservice.TxValidationStatus, numTx),
	}

	// collect token transactions
	for i := uint64(0); i < numTx; i++ {
		tx := <-txQueue

		txSeqNum := token.TxSeqNum{BlkNum: blockNo, TxNum: i}
		txStatus := conflicts.ApplyConflicts(txSeqNum, tx)

		b.Block.Txs[i] = tx
		b.ExpectedResults[i] = &coordinatorservice.TxValidationStatus{
			BlockNum: txSeqNum.BlkNum,
			TxNum:    txSeqNum.TxNum,
			Status:   txStatus,
		}
	}

	return b
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
