package workload

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

const snFormatter = "%s-%d"

func Generate(profilePath, outputPath string) {

	pp := LoadProfileFromYaml(profilePath)

	publicKey, signer, txQueue := startTxGenerator(pp)
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

	// prepare conflictsHelper
	conflicts := newConflicts(pp, signer.SignTx)

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
		bar := NewProgressBar("Writing blocks to file...", blockCount)
		BlockWriter(writer, bQueue, func() {
			bar.Add(1)
		})
	}()

	for blockNo := int64(1); blockNo <= blockCount; blockNo++ {
		bQueue <- createBlock(blockNo, blockSize, txQueue, conflicts)
	}
	close(bQueue)
	wg.Wait()
}

func createBlock(blockNo, numTx int64, txQueue chan *token.Tx, conflicts *conflictsHelper) *BlockWithExpectedResult {

	b := &BlockWithExpectedResult{
		Block: &token.Block{
			Number: uint64(blockNo),
			Txs:    make([]*token.Tx, numTx),
		},
		ExpectedResults: make([]*coordinatorservice.TxValidationStatus, numTx),
	}

	// collect token transactions
	for i := int64(0); i < numTx; i++ {
		tx := <-txQueue

		txStatus := applyConflicts(blockNo, i, conflicts, tx)

		b.Block.Txs[i] = tx
		b.ExpectedResults[i] = &coordinatorservice.TxValidationStatus{
			BlockNum: uint64(blockNo),
			TxNum:    uint64(i),
			Status:   txStatus,
		}
	}

	return b
}

type conflictsHelper struct {
	isSigConflict     map[string]bool
	hasDoubleConflict map[string][]int
	doublesMapping    map[string]string
	doubles           map[string][]byte
	signFnc           func([][]byte) ([]byte, error)
}

func newConflicts(pp *Profile, signerFnc func([][]byte) ([]byte, error)) *conflictsHelper {

	// TODO let's think about a different construction of this conflict mapper
	// as Alex suggested we could use `map[BlkNum]map[TxNum]map[SnNum]SerialNumber`

	c := &conflictsHelper{
		// TODO find better name for these helpers
		isSigConflict:     make(map[string]bool),   // txid to invalid sig
		hasDoubleConflict: make(map[string][]int),  // txid to list of sn positions
		doublesMapping:    make(map[string]string), // txid to snid
		doubles:           make(map[string][]byte), // snid to bytes
		signFnc:           signerFnc,
	}

	for txid, o := range pp.Conflicts {
		if o.InvalidSignature {
			c.isSigConflict[txid] = true
		}

		if len(o.DoubleSpends) == 0 {
			continue
		}

		// current txid

		var sns []int
		for sn, targetSnid := range o.DoubleSpends {
			snid := fmt.Sprintf(snFormatter, txid, sn)
			// sanity check targetSnid < snid
			if !inThePast(snid, targetSnid) {
				panic("INVALID Your conflict does not make sense. " + snid + " references " + targetSnid)
			}

			sns = append(sns, sn)
			c.doubles[targetSnid] = nil
			c.doublesMapping[snid] = targetSnid
		}
		c.hasDoubleConflict[txid] = sns
	}

	return c
}

// inThePast checks that transaction a is before transaction a.
// Returns true if b < a; otherwise false
func inThePast(a, b string) bool {
	aaa := strings.Split(a, "-")
	bbb := strings.Split(b, "-")

	if len(aaa) != len(bbb) && len(aaa) != 3 {
		panic("inThePast inputs are bad! a: " + a + " b: " + b)
	}

	toNum := func(s string) int64 {
		num, err := strconv.ParseInt(s, 10, 64)
		utils.Must(err)
		return num
	}

	aBlNum, bBlNum := toNum(aaa[0]), toNum(bbb[0])
	aTxNum, bTxNum := toNum(aaa[1]), toNum(bbb[1])
	// note that we don't care about the sn here

	return bBlNum <= aBlNum && bTxNum < aTxNum
}

// applyConflicts modifies tx if a conflict is specified within the configHelper.
// If a conflict is applied, the corresponding status is returned; otherwise it returns valid status
func applyConflicts(blockNum int64, txNum int64, conflicts *conflictsHelper, tx *token.Tx) coordinatorservice.Status {
	if conflicts == nil {
		// let's do nothing if there are no conflicts specified for this run
		return coordinatorservice.Status_VALID
	}

	txid := fmt.Sprintf("%d-%d", blockNum, txNum)

	// collect sn as double candidate as we need to use it later
	for i, sn := range tx.SerialNumbers {
		// store the sn for later if we need it
		snid := fmt.Sprintf(snFormatter, txid, i)
		if _, ok := conflicts.doubles[snid]; ok {
			conflicts.doubles[snid] = sn
		}
	}

	// note that we currently create conflicts which are either INVALID_SIGNATURE or DOUBLE_SPEND

	// apply signature conflicts
	if _, ok := conflicts.isSigConflict[txid]; ok {
		sigverification_test.Reverse(tx.Signature)
		return coordinatorservice.Status_INVALID_SIGNATURE
	}

	// apply double spends
	if sns, ok := conflicts.hasDoubleConflict[txid]; ok {
		// replace
		for _, j := range sns {
			snid := fmt.Sprintf(snFormatter, txid, j)
			targetSnid, ok := conflicts.doublesMapping[snid]
			if !ok {
				panic("mama mia! cannot apply conflict as we have not seen" + targetSnid)
			}

			// lookup
			newSn, ok := conflicts.doubles[targetSnid]
			if !ok {
				panic("ohhh gosh: " + snid + " cannot find")
			}

			// replace
			tx.SerialNumbers[j-1] = newSn
		}
		// re-sign
		var err error
		tx.Signature, err = conflicts.signFnc(tx.SerialNumbers)
		utils.Must(err)

		return coordinatorservice.Status_DOUBLE_SPEND
	}

	return coordinatorservice.Status_VALID
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
