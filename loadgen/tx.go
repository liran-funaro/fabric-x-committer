package loadgen

import (
	"math/rand"

	"github.com/google/uuid"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

// IndependentTxGenerator generates a new valid TX given key generators.
type IndependentTxGenerator struct {
	ReadOnlyKeyGenerator     Generator[[][]byte]
	ReadWriteKeyGenerator    Generator[[][]byte]
	BlindWriteKeyGenerator   Generator[[][]byte]
	BlindWriteValueGenerator Generator[[][]byte]
}

// newIndependentTxGenerator creates a new valid TX generator given a transaction profile.
func newIndependentTxGenerator(rnd *rand.Rand, profile *TransactionProfile) *IndependentTxGenerator {
	return &IndependentTxGenerator{
		ReadOnlyKeyGenerator:     keyGenerator(rnd, profile.KeySize, profile.ReadOnlyCount),
		ReadWriteKeyGenerator:    keyGenerator(rnd, profile.KeySize, profile.ReadWriteCount),
		BlindWriteKeyGenerator:   keyGenerator(rnd, profile.KeySize, profile.BlindWriteCount),
		BlindWriteValueGenerator: keyGenerator(rnd, profile.BlindWriteValueSize, profile.BlindWriteCount),
	}
}

// Next generate a new TX.
func (g *IndependentTxGenerator) Next() *protoblocktx.Tx {
	readOnly := g.ReadOnlyKeyGenerator.Next()
	readWrite := g.ReadWriteKeyGenerator.Next()
	blindWriteKey := g.BlindWriteKeyGenerator.Next()
	blindWriteValue := g.BlindWriteKeyGenerator.Next()

	tx := &protoblocktx.Tx{
		Id: uuid.New().String(),
		Namespaces: []*protoblocktx.TxNamespace{
			{
				NsId:        0,
				ReadsOnly:   make([]*protoblocktx.Read, len(readOnly)),
				ReadWrites:  make([]*protoblocktx.ReadWrite, len(readWrite)),
				BlindWrites: make([]*protoblocktx.Write, len(blindWriteKey)),
			},
		},
	}

	for i, key := range readOnly {
		tx.Namespaces[0].ReadsOnly[i] = &protoblocktx.Read{Key: key}
	}

	for i, key := range readWrite {
		tx.Namespaces[0].ReadWrites[i] = &protoblocktx.ReadWrite{Key: key}
	}

	for i, key := range blindWriteKey {
		tx.Namespaces[0].BlindWrites[i] = &protoblocktx.Write{
			Key:   key,
			Value: blindWriteValue[i],
		}
	}

	return tx
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

// BlockGenerator generates new blocks given a TX generator.
type BlockGenerator struct {
	TxGenerator *TxStreamGenerator
	BlockSize   uint64
	blockNum    uint64
}

// Next generate a new block.
func (g *BlockGenerator) Next() *protoblocktx.Block {
	block := &protoblocktx.Block{
		Number: g.blockNum,
		Txs:    g.TxGenerator.NextN(int(g.BlockSize)),
	}
	g.blockNum++
	return block
}
