package loadgen

import (
	"math/rand"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

type (
	// TxStreamGenerator is a generator of TXs.
	TxStreamGenerator struct {
		TxQueue <-chan *protoblocktx.Tx
		Signer  *TxSignerVerifier
	}

	// BlockStreamGenerator is a generator of blocks.
	BlockStreamGenerator struct {
		BlockQueue <-chan *protoblocktx.Block
		Signer     *TxSignerVerifier
	}
)

// StartTxGenerator starts workers that generates TXs into a queue.
func StartTxGenerator(profile *Profile) *TxStreamGenerator {
	seedRnd := rand.New(rand.NewSource(profile.Seed))
	logger.Debugf("Starting %d workers to generate load", profile.TxGenWorkers)

	indTxQueue := make(chan *protoblocktx.Tx, profile.Transaction.BufferSize)
	for i := uint32(0); i < Max(profile.TxGenWorkers, 1); i++ {
		txGen := newIndependentTxGenerator(workerRnd(seedRnd), &profile.Transaction)
		go generateTx(txGen, indTxQueue)
	}

	depTxQueue := make(chan *protoblocktx.Tx, profile.Transaction.BufferSize)
	for i := uint32(0); i < Max(profile.TxDependenciesWorkers, 1); i++ {
		txGen := newTxDependenciesDecorator(workerRnd(seedRnd), indTxQueue, profile)
		go generateTx(txGen, depTxQueue)
	}

	txQueue := make(chan *protoblocktx.Tx, profile.Transaction.BufferSize)
	signer := NewTxSignerVerifier(&profile.Transaction.Signature)
	for i := uint32(0); i < Max(profile.TxSignWorkers, 1); i++ {
		txGen := newSignTxDecorator(workerRnd(seedRnd), depTxQueue, signer, profile)
		go generateTx(txGen, txQueue)
	}

	return &TxStreamGenerator{
		TxQueue: txQueue,
		Signer:  signer,
	}
}

// NextN returns the next N TXs.
func (txGen *TxStreamGenerator) NextN(num int) []*protoblocktx.Tx {
	txs := make([]*protoblocktx.Tx, num)
	for i := 0; i < num; i++ {
		txs[i] = <-txGen.TxQueue
	}
	return txs
}

// StartBlockGenerator starts workers that generates blocks into a queue.
func StartBlockGenerator(profile *Profile) *BlockStreamGenerator {
	txGen := StartTxGenerator(profile)
	blockGen := &BlockGenerator{
		TxGenerator: txGen,
		BlockSize:   uint64(profile.Block.Size),
	}

	blockQueue := make(chan *protoblocktx.Block, profile.Block.BufferSize)
	go func() {
		for {
			blockQueue <- blockGen.Next()
		}
	}()

	return &BlockStreamGenerator{
		BlockQueue: blockQueue,
		Signer:     txGen.Signer,
	}
}

func workerRnd(seedRnd *rand.Rand) *rand.Rand {
	return rand.New(rand.NewSource(seedRnd.Int63()))
}

type gen interface {
	Next() *protoblocktx.Tx
}

func generateTx(gen gen, txQueue chan<- *protoblocktx.Tx) {
	for {
		txQueue <- gen.Next()
	}
}
