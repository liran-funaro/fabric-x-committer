package workload

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

func StartTxGenerator(profile *TransactionProfile, conflicts *ConflictProfile, bufferSize int64) (signature.PublicKey, chan *sigverification_test.TxWithStatus) {
	privateKey, publicKey, _ := sigverification_test.ReadOrGenerateKeys(profile.Signature)
	signer, _ := sigverification_test.GetSignatureFactory(profile.Signature.Scheme).NewSigner(privateKey)

	g := NewConflictDecorator(&sigverification_test.ValidTxGenerator{
		TxSigner:                signer,
		TxSerialNumberGenerator: sigverification_test.NewLinearTxInputGenerator(profile.SerialNumberSize),
		TxOutputGenerator:       sigverification_test.NewLinearTxInputGenerator(profile.OutputSize),
	}, conflicts, signer.SignTx)

	numWorker := runtime.NumCPU()
	w := os.Getenv("NUM_WORKERS")
	if w != "" {
		n, err := strconv.Atoi(w)
		utils.Must(err)
		numWorker = n
	}

	txQueueSize := numWorker * int(bufferSize)

	// start workers already to produce transactions
	txQueue := make(chan *sigverification_test.TxWithStatus, txQueueSize)
	fmt.Printf("Starting %d worker to generate load\n", numWorker)
	for i := 0; i < numWorker; i++ {
		go func() {
			for {
				txQueue <- g.Next()
			}
		}()
	}

	return publicKey, txQueue
}

func StartBlockGenerator(pp *Profile) (signature.PublicKey, <-chan *BlockWithExpectedResult) {
	pk, txQueue := StartTxGenerator(&pp.Transaction, pp.Conflicts, pp.Block.Size)

	blockSize := uint64(pp.Block.Size)

	bQueue := make(chan *BlockWithExpectedResult, 1000)
	blockNo := uint64(0)
	go func() {
		for {
			bQueue <- createBlock(blockNo, blockSize, txQueue)
			blockNo++
		}
	}()

	return pk, bQueue
}
