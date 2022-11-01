package workload

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

func startTxGenerator(pp *Profile) (signature.PublicKey, sigverification_test.TxSigner, chan *token.Tx) {

	sigType := strings.ToUpper(pp.Transaction.Signature.Type)

	privateKey, publicKey := sigverification_test.GetSignatureFactory(sigType).NewKeys()
	signer, _ := sigverification_test.GetSignatureFactory(sigType).NewSigner(privateKey)

	g := &sigverification_test.TxGenerator{
		TxSigner:               signer,
		TxInputGenerator:       sigverification_test.NewLinearTxInputGenerator(pp.Transaction.SerialNumber.Count),
		ValidSigRatioGenerator: test.NewBooleanGenerator(test.PercentageUniformDistribution, test.Percentage(pp.Transaction.Signature.ValidityRatio), 10),
	}

	blockSize := pp.Block.Size
	numWorker := runtime.NumCPU()
	w := os.Getenv("NUM_WORKERS")
	if w != "" {
		n, err := strconv.Atoi(w)
		utils.Must(err)
		numWorker = n
	}

	txQueueSize := numWorker * int(blockSize)

	// start workers already to produce transactions
	txQueue := make(chan *token.Tx, txQueueSize)
	fmt.Printf("Starting %d worker to generate load\n", numWorker)
	for i := 0; i < numWorker; i++ {
		// we already start our workers to produce some transactions
		go func() {
			for {
				// create transactions
				tx, _ := g.Next()
				txQueue <- tx
			}
		}()
	}

	return publicKey, signer, txQueue
}

func StartBlockGenerator(pp *Profile) (signature.PublicKey, chan *BlockWithExpectedResult) {
	pk, _, txQueue := startTxGenerator(pp)

	blockSize := pp.Block.Size
	queueBufferSize := 1000

	bQueue := make(chan *BlockWithExpectedResult, queueBufferSize)

	blockNo := int64(0)
	go func() {
		for {
			bQueue <- createBlock(blockNo, blockSize, txQueue, nil)
			blockNo++
		}
	}()

	return pk, bQueue
}
