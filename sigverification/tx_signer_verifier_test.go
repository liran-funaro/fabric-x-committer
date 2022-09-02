package sigverification

import (
	"testing"

	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

type environmentParams struct {
	scheme           Scheme
	valid            test.Percentage
	txSize           *test.NormalDistribution
	serialNumberSize *test.NormalDistribution
}

func newTestArgGenerator(params *environmentParams) *testArgGenerator {
	txSigner := NewTxSigner(params.scheme)
	publicKey, privateKey := txSigner.NewKeys()
	return &testArgGenerator{
		txSigner:                  txSigner,
		privateKey:                privateKey,
		PublicKey:                 publicKey,
		txSizeGenerator:           test.NewPositiveIntGenerator(params.txSize, 50),
		serialNumberSizeGenerator: test.NewPositiveIntGenerator(params.serialNumberSize, 50),
		serialNumberGenerator:     test.NewFastByteArrayGenerator(60),
		validGenerator:            test.NewBooleanGenerator(test.PercentageUniformDistribution, params.valid, 10),
	}
}

type testArgGenerator struct {
	txSigner   TxSigner
	privateKey PrivateKey
	PublicKey  PublicKey

	txSizeGenerator           *test.PositiveIntGenerator
	serialNumberSizeGenerator *test.PositiveIntGenerator
	serialNumberGenerator     *test.FastByteArrayGenerator
	validGenerator            *test.BooleanGenerator
}

func (g *testArgGenerator) NextTx() *token.Tx {
	serialNumbers := g.nextSerialNumbers()
	var signature Signature
	if g.validGenerator.Next() {
		signature, _ = g.txSigner.SignTx(g.privateKey, serialNumbers)
	}
	return &token.Tx{SerialNumbers: serialNumbers, Signature: signature}
}

func (g *testArgGenerator) nextSerialNumbers() []SerialNumber {
	txSize := g.txSizeGenerator.Next()
	serialNumbers := make([]SerialNumber, txSize)
	for i := 0; i < txSize; i++ {
		serialNumbers[i] = g.serialNumberGenerator.Next(g.serialNumberSizeGenerator.Next())
	}
	return serialNumbers
}

var c = &environmentParams{
	scheme:           Ecdsa,
	valid:            1,
	txSize:           test.Stable(100),
	serialNumberSize: test.Stable(1000),
}

func BenchmarkNewParallelExecutor(b *testing.B) {
	g := newTestArgGenerator(c)
	txVerifier := NewTxVerifier(c.scheme)
	publicKey := g.PublicKey

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		txVerifier.VerifyTx(publicKey, g.NextTx())
	}
}
