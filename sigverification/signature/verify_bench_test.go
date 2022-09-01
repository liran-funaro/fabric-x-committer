package signature_test

import (
	"testing"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/testutils"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

var benchmarkConfigs = []struct {
	name                 string
	inputGeneratorParams *inputGeneratorParams
}{{
	name: "basic",
	inputGeneratorParams: &inputGeneratorParams{&testutils.TxGeneratorParams{
		Scheme:           signature.Ecdsa,
		ValidSigRatio:    test.Always,
		TxSize:           test.Uniform(1, 10),
		SerialNumberSize: test.Constant(64),
	}},
}}

func BenchmarkTxVerifier(b *testing.B) {
	for _, config := range benchmarkConfigs {
		b.Run(config.name, func(b *testing.B) {
			g := NewInputGenerator(config.inputGeneratorParams)
			txVerifier := signature.NewTxVerifier(config.inputGeneratorParams.Scheme)
			publicKey := g.PublicKey()

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				txVerifier.VerifyTx(publicKey, g.NextTx())
			}
		})
	}
}

// Input generator

type inputGeneratorParams struct {
	*testutils.TxGeneratorParams
}
type inputGenerator struct {
	txGenerator testutils.TxGenerator
}

func NewInputGenerator(params *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		txGenerator: *testutils.NewTxGenerator(params.TxGeneratorParams),
	}
}

func (g *inputGenerator) PublicKey() signature.PublicKey {
	return g.txGenerator.PublicKey
}

func (g *inputGenerator) NextTx() *token.Tx {
	return g.txGenerator.Next()
}
