package loadgen

import (
	"fmt"
	"math/rand"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

const (
	dependencyReadOnly   = "read"
	dependencyReadWrite  = "read-write"
	dependencyBlindWrite = "write"
)

type (
	signTxDecorator struct {
		Signer               *TxSignerVerifier
		txQueue              <-chan *protoblocktx.Tx
		invalidSignGenerator Generator[bool]
		invalidSignature     []byte
	}

	dependenciesDecorator struct {
		txQueue         <-chan *protoblocktx.Tx
		keyGenerator    Generator[[]byte]
		dependencies    []dependencyDesc
		dependenciesMap map[uint64][]dependency
		index           uint64
	}

	dependencyDesc struct {
		bernoulliGenerator Generator[int]
		gapGenerator       Generator[int]
		src                string
		dst                string
	}

	dependency struct {
		key []byte
		src string
		dst string
	}
)

// newSignTxDecorator wraps a TX generator and signs it given the conflicts profile.
func newSignTxDecorator(
	rnd *rand.Rand, txQueue <-chan *protoblocktx.Tx, signer *TxSignerVerifier, profile *Profile,
) *signTxDecorator {
	dist := NewBernoulliDistribution(profile.Conflicts.InvalidSignatures)
	var invalidTx protoblocktx.Tx
	signer.Sign(&invalidTx)
	return &signTxDecorator{
		Signer:               signer,
		txQueue:              txQueue,
		invalidSignGenerator: dist.MakeBooleanGenerator(rnd),
		invalidSignature:     invalidTx.Signature,
	}
}

// Next generate a new TX.
func (g *signTxDecorator) Next() *protoblocktx.Tx {
	tx := <-g.txQueue
	if g.invalidSignGenerator.Next() {
		tx.Signature = g.invalidSignature
	} else {
		g.Signer.Sign(tx)
	}
	return tx
}

// newTxDependenciesDecorator wraps a tx generator and adds dependencies conflicts.
func newTxDependenciesDecorator(
	rnd *rand.Rand, txQueue <-chan *protoblocktx.Tx, profile *Profile,
) *dependenciesDecorator {
	return &dependenciesDecorator{
		txQueue:      txQueue,
		keyGenerator: &ByteArrayGenerator{Size: profile.Transaction.KeySize, Rnd: rnd},
		dependencies: Map(profile.Conflicts.Dependencies, func(
			_ int, value DependencyDescription,
		) dependencyDesc {
			var gapGen Generator[int]
			if value.Gap == nil {
				gapGen = &ConstGenerator[int]{Const: 1}
			} else {
				gapGen = value.Gap.MakePositiveIntGenerator(rnd)
			}
			return dependencyDesc{
				bernoulliGenerator: &FloatToIntGenerator{FloatGen: &BernoulliGenerator{
					Rnd:         rnd,
					Probability: value.Probability,
				}},
				gapGenerator: gapGen,
				src:          value.Src,
				dst:          value.Dst,
			}
		}),
		dependenciesMap: make(map[uint64][]dependency),
	}
}

// Next generate a new tx.
func (g *dependenciesDecorator) Next() *protoblocktx.Tx {
	tx := <-g.txQueue
	depList, ok := g.dependenciesMap[g.index]
	if ok {
		delete(g.dependenciesMap, g.index)
		for _, d := range depList {
			addKey(tx, d.dst, d.key)
		}
	}

	for _, depDesc := range g.dependencies {
		if depDesc.bernoulliGenerator.Next() != 1 {
			continue
		}

		gap := uint64(Max(depDesc.gapGenerator.Next(), 1))
		d := dependency{
			key: g.keyGenerator.Next(),
			src: depDesc.src,
			dst: depDesc.dst,
		}
		addKey(tx, d.src, d.key)
		g.dependenciesMap[g.index+gap] = append(g.dependenciesMap[g.index+gap], d)
	}

	g.index++
	return tx
}

func addKey(tx *protoblocktx.Tx, dependencyType string, key []byte) {
	txNs := tx.Namespaces[0]
	switch dependencyType {
	case dependencyReadOnly:
		txNs.ReadsOnly = append(txNs.ReadsOnly, &protoblocktx.Read{Key: key})
	case dependencyReadWrite:
		txNs.ReadWrites = append(txNs.ReadWrites, &protoblocktx.ReadWrite{Key: key})
	case dependencyBlindWrite:
		txNs.BlindWrites = append(txNs.BlindWrites, &protoblocktx.Write{Key: key})
	default:
		panic(fmt.Sprintf("invalid dependency type: %s", dependencyType))
	}
}
