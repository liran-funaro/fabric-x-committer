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

func StartTxGenerator(profile *TransactionProfile, bufferSize int64) (signature.PublicKey, sigverification_test.TxSigner, chan *token.Tx) {
	sigType := strings.ToUpper(profile.SignatureType)

	privateKey, publicKey := sigverification_test.GetSignatureFactory(sigType).NewKeys()
	signer, _ := sigverification_test.GetSignatureFactory(sigType).NewSigner(privateKey)

	g := &sigverification_test.TxGenerator{
		TxSigner:               signer,
		TxInputGenerator:       sigverification_test.NewLinearTxInputGenerator(profile.Size),
		ValidSigRatioGenerator: test.NewBooleanGenerator(test.PercentageUniformDistribution, test.Always, 10),
	}

	numWorker := runtime.NumCPU()
	w := os.Getenv("NUM_WORKERS")
	if w != "" {
		n, err := strconv.Atoi(w)
		utils.Must(err)
		numWorker = n
	}

	txQueueSize := numWorker * int(bufferSize)

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
	pk, signer, txQueue := StartTxGenerator(&pp.Transaction, pp.Block.Size)

	blockSize := uint64(pp.Block.Size)
	queueBufferSize := 1000

	bQueue := make(chan *BlockWithExpectedResult, queueBufferSize)

	conflictHandler := NewConflictHandler(nil, pp.Conflicts.Statistical, signer.SignTx)
	blockNo := uint64(0)
	go func() {
		for {
			bQueue <- createBlock(blockNo, blockSize, txQueue, conflictHandler)
			blockNo++
		}
	}()

	return pk, bQueue
}
