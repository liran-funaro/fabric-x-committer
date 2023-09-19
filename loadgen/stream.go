package loadgen

import (
	"math/rand"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

// TxStreamGenerator generates TX or a block.
// It includes the signer to support verifications.
type TxStreamGenerator[T any] struct {
	ChanGenerator[T]
	Signer *TxSignerVerifier
}

func workerRnd(seedRnd *rand.Rand) *rand.Rand {
	return rand.New(rand.NewSource(seedRnd.Int63()))
}

// StartTxGenerator starts workers that generates TXs into a queue.
func StartTxGenerator(profile *Profile) *TxStreamGenerator[*protoblocktx.Tx] {
	seedRnd := rand.New(rand.NewSource(profile.Seed))
	logger.Debugf("Starting %d workers to generate load", profile.TxGenWorkers)

	indTxQueue := make(chan *protoblocktx.Tx, profile.Transaction.BufferSize)
	for i := uint32(0); i < Max(profile.TxGenWorkers, 1); i++ {
		txGen := NewIndependentTxGenerator(workerRnd(seedRnd), &profile.Transaction)
		go GeneratorWorker(txGen, indTxQueue)
	}

	indTxGen := &ChanGenerator[*protoblocktx.Tx]{Chan: indTxQueue}
	depTxQueue := make(chan *protoblocktx.Tx, profile.Transaction.BufferSize)
	for i := uint32(0); i < Max(profile.TxDependenciesWorkers, 1); i++ {
		txGen := NewTxDependenciesDecorator(workerRnd(seedRnd), indTxGen, profile)
		go GeneratorWorker(txGen, depTxQueue)
	}

	depTxGen := &ChanGenerator[*protoblocktx.Tx]{Chan: depTxQueue}
	txQueue := make(chan *protoblocktx.Tx, profile.Transaction.BufferSize)
	signer := NewTxSignerVerifier(&profile.Transaction.Signature)
	for i := uint32(0); i < Max(profile.TxSignWorkers, 1); i++ {
		txGen := NewSignTxDecorator(workerRnd(seedRnd), depTxGen, signer, profile)
		go GeneratorWorker(txGen, txQueue)
	}

	return &TxStreamGenerator[*protoblocktx.Tx]{
		ChanGenerator: ChanGenerator[*protoblocktx.Tx]{Chan: txQueue},
		Signer:        signer,
	}
}

// StartBlockGenerator starts workers that generates blocks into a queue.
func StartBlockGenerator(profile *Profile) *TxStreamGenerator[*protoblocktx.Block] {
	txGen := StartTxGenerator(profile)
	var blockGen Generator[*protoblocktx.Block] = &BlockGenerator{
		TxGenerator: txGen,
		BlockSize:   uint64(profile.Block.Size),
	}

	blockQueue := make(chan *protoblocktx.Block, profile.Block.BufferSize)
	go GeneratorWorker(blockGen, blockQueue)
	return &TxStreamGenerator[*protoblocktx.Block]{
		ChanGenerator: ChanGenerator[*protoblocktx.Block]{Chan: blockQueue},
		Signer:        txGen.Signer,
	}
}
