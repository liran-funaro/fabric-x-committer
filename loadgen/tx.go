package loadgen

import (
	"math/rand"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

// IndependentTxGenerator generates a new valid TX given key generators.
type IndependentTxGenerator struct {
	ReadOnlyKeyGenerator   Generator[[][]byte]
	ReadWriteKeyGenerator  Generator[[][]byte]
	BlindWriteKeyGenerator Generator[[][]byte]
}

func keyGenerator(rnd *rand.Rand, keySize uint32, keyCount *Distribution) *MultiGenerator[[]byte] {
	ret := &MultiGenerator[[]byte]{
		Gen: &ByteArrayGenerator{Size: keySize, Rnd: rnd},
	}

	if keyCount == nil {
		ret.Count = &ConstGenerator[int]{Const: 0}
	} else {
		ret.Count = keyCount.MakeIntGenerator(rnd)
	}

	return ret
}

// NewIndependentTxGenerator creates a new valid TX generator given a transaction profile.
func NewIndependentTxGenerator(rnd *rand.Rand, profile *TransactionProfile) Generator[*protoblocktx.Tx] {
	return &IndependentTxGenerator{
		ReadOnlyKeyGenerator:   keyGenerator(rnd, profile.KeySize, profile.ReadOnlyCount),
		ReadWriteKeyGenerator:  keyGenerator(rnd, profile.KeySize, profile.ReadWriteCount),
		BlindWriteKeyGenerator: keyGenerator(rnd, profile.KeySize, profile.BlindWriteCount),
	}
}

// Next generate a new TX.
func (g *IndependentTxGenerator) Next() *protoblocktx.Tx {
	readOnly := g.ReadOnlyKeyGenerator.Next()
	readWrite := g.ReadWriteKeyGenerator.Next()
	blindWrite := g.BlindWriteKeyGenerator.Next()

	tx := &protoblocktx.Tx{
		Namespaces: []*protoblocktx.TxNamespace{
			{
				NsId:        0,
				ReadsOnly:   make([]*protoblocktx.Read, len(readOnly)),
				ReadWrites:  make([]*protoblocktx.ReadWrite, len(readWrite)),
				BlindWrites: make([]*protoblocktx.Write, len(blindWrite)),
			},
		},
	}

	for i, key := range readOnly {
		tx.Namespaces[0].ReadsOnly[i] = &protoblocktx.Read{Key: key}
	}

	for i, key := range readWrite {
		tx.Namespaces[0].ReadWrites[i] = &protoblocktx.ReadWrite{Key: key}
	}

	for i, key := range blindWrite {
		tx.Namespaces[0].BlindWrites[i] = &protoblocktx.Write{Key: key}
	}

	return tx
}

// BlockGenerator generates new blocks given a TX generator.
type BlockGenerator struct {
	TxGenerator Generator[*protoblocktx.Tx]
	BlockSize   uint64
	blockNum    uint64
}

// Next generate a new block.
func (g *BlockGenerator) Next() *protoblocktx.Block {
	block := &protoblocktx.Block{
		Number: g.blockNum,
		Txs:    GenerateArray(g.TxGenerator, int(g.BlockSize)),
	}
	g.blockNum++
	return block
}
