package loadgen

import (
	"encoding/json"
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
func StartTxGenerator(profile *Profile, limiterConfig LimiterConfig) *TxStreamGenerator {
	seedRnd := rand.New(rand.NewSource(profile.Seed))
	logger.Debugf("Starting %d workers to generate load", profile.TxGenWorkers)

	indTxQueue := make(chan *protoblocktx.Tx, profile.Transaction.BufferSize)
	for i := uint32(0); i < Max(profile.TxGenWorkers, 1); i++ {
		txGen := newIndependentTxGenerator(NewRandFromSeedGenerator(seedRnd), &profile.Transaction)
		go func() {
			for {
				indTxQueue <- txGen.Next()
			}
		}()
	}

	depTxQueue := make(chan *protoblocktx.Tx, profile.Transaction.BufferSize)
	for i := uint32(0); i < Max(profile.TxDependenciesWorkers, 1); i++ {
		txGen := newTxDependenciesDecorator(NewRandFromSeedGenerator(seedRnd), indTxQueue, profile)
		go func() {
			for {
				depTxQueue <- txGen.Next()
			}
		}()
	}

	txQueue := make(chan *protoblocktx.Tx, profile.Transaction.BufferSize)
	signer := NewTxSignerVerifier(&profile.Transaction.Signature)
	rateLimiter := NewLimiter(&limiterConfig)
	for i := uint32(0); i < Max(profile.TxSignWorkers, 1); i++ {
		txGen := newSignTxDecorator(NewRandFromSeedGenerator(seedRnd), depTxQueue, signer, profile)
		go func() {
			for {
				txQueue <- txGen.Next()
				rateLimiter.Take()
			}
		}()
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
func StartBlockGenerator(profile *Profile, limiterConfig LimiterConfig) *BlockStreamGenerator {
	p, _ := json.Marshal(profile)
	logger.Infof("Profile passed: %s", string(p))
	txGen := StartTxGenerator(profile, limiterConfig)
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
