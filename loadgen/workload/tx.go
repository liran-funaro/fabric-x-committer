package workload

import (
	"math/rand"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
)

// IndependentTxGenerator generates a new valid TX given key generators.
type (
	IndependentTxGenerator struct {
		TxIDGenerator            *UUIDGenerator
		ReadOnlyKeyGenerator     *MultiGenerator[[]byte]
		ReadWriteKeyGenerator    *MultiGenerator[[]byte]
		BlindWriteKeyGenerator   *MultiGenerator[[]byte]
		ReadWriteValueGenerator  *ByteArrayGenerator
		BlindWriteValueGenerator *ByteArrayGenerator
	}

	txModifierDecorator struct {
		txGen     *IndependentTxGenerator
		modifiers []Modifier
	}

	Modifier interface {
		Modify(*protoblocktx.Tx) (*protoblocktx.Tx, error)
	}
)

// GeneratedNamespaceID for now we're only generating transactions for a single namespace.
const GeneratedNamespaceID = "0"

// newIndependentTxGenerator creates a new valid TX generator given a transaction profile.
func newIndependentTxGenerator(
	rnd *rand.Rand, keys *ByteArrayGenerator, profile *TransactionProfile,
) *IndependentTxGenerator {
	return &IndependentTxGenerator{
		TxIDGenerator:            &UUIDGenerator{Rnd: rnd},
		ReadOnlyKeyGenerator:     multiKeyGenerator(rnd, keys, profile.ReadOnlyCount),
		ReadWriteKeyGenerator:    multiKeyGenerator(rnd, keys, profile.ReadWriteCount),
		BlindWriteKeyGenerator:   multiKeyGenerator(rnd, keys, profile.BlindWriteCount),
		ReadWriteValueGenerator:  valueGenerator(rnd, profile.ReadWriteValueSize),
		BlindWriteValueGenerator: valueGenerator(rnd, profile.BlindWriteValueSize),
	}
}

// Next generate a new TX.
func (g *IndependentTxGenerator) Next() *protoblocktx.Tx {
	readOnly := g.ReadOnlyKeyGenerator.Next()
	readWrite := g.ReadWriteKeyGenerator.Next()
	blindWriteKey := g.BlindWriteKeyGenerator.Next()

	tx := &protoblocktx.Tx{
		Id: g.TxIDGenerator.Next(),
		Namespaces: []*protoblocktx.TxNamespace{
			{
				NsId:        GeneratedNamespaceID,
				NsVersion:   types.VersionNumber(0).Bytes(),
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
		tx.Namespaces[0].ReadWrites[i] = &protoblocktx.ReadWrite{
			Key:   key,
			Value: g.ReadWriteValueGenerator.Next(),
		}
	}

	for i, key := range blindWriteKey {
		tx.Namespaces[0].BlindWrites[i] = &protoblocktx.Write{
			Key:   key,
			Value: g.BlindWriteValueGenerator.Next(),
		}
	}

	return tx
}

// Key is an alias for byte array.
type Key = []byte

func multiKeyGenerator(rnd *rand.Rand, keyGen Generator[Key], keyCount *Distribution) *MultiGenerator[Key] {
	ret := &MultiGenerator[Key]{Gen: keyGen}

	if keyCount == nil {
		ret.Count = &ConstGenerator[int]{Const: 0}
	} else {
		ret.Count = keyCount.MakeIntGenerator(rnd)
	}

	return ret
}

func valueGenerator(rnd *rand.Rand, valueSize uint32) *ByteArrayGenerator {
	return &ByteArrayGenerator{Size: valueSize, Rnd: rnd}
}

// BlockGenerator generates new blocks given a TX generator.
// It does not set the block number. It is to be set by the receiver.
type BlockGenerator struct {
	TxGenerator Generator[*protoblocktx.Tx]
	BlockSize   uint64
	txNums      []uint32
}

// Next generate a new block.
func (g *BlockGenerator) Next() *protocoordinatorservice.Block {
	txs := NextN(g.TxGenerator, int(g.BlockSize)) //nolint:gosec // integer overflow conversion uint64 -> int
	// Generators return nil when their stream is done.
	// This indicates that the block generator should also be done.
	if txs[len(txs)-1] == nil {
		return nil
	}

	// Lazy initialization of the tx numbers.
	if len(g.txNums) < len(txs) {
		g.txNums = utils.Range(0, uint32(len(txs))) //nolint:gosec // integer overflow conversion int -> uint32
	}

	return &protocoordinatorservice.Block{
		Txs:    txs,
		TxsNum: g.txNums[:len(txs)],
	}
}

// newTxModifierTxDecorator wraps a TX generator and apply one or more modification methods.
func newTxModifierTxDecorator(txGen *IndependentTxGenerator, modifiers ...Modifier) *txModifierDecorator {
	return &txModifierDecorator{
		txGen:     txGen,
		modifiers: modifiers,
	}
}

// Next apply all the modifiers to a transaction.
func (g *txModifierDecorator) Next() *protoblocktx.Tx {
	tx := g.txGen.Next()
	var err error
	for _, mod := range g.modifiers {
		tx, err = mod.Modify(tx)
		if err != nil {
			logger.Infof("Failed modifiying TX with error: %s", err)
		}
	}
	return tx
}
