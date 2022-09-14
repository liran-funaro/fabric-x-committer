package signature_test

import (
	"fmt"
	"testing"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

type benchmarkConfig struct {
	Name                 string
	InputGeneratorParams *inputGeneratorParams
	VerificationScheme   signature.Scheme
}

var baseConfig = benchmarkConfig{
	Name: "basic",
	InputGeneratorParams: &inputGeneratorParams{
		TxInputGeneratorParams: &sigverification_test.TxInputGeneratorParams{
			TxSize:           test.Constant(1),
			SerialNumberSize: test.Constant(64),
		},
		ValidSigRatio: test.Always,
	},
	VerificationScheme: signature.Ecdsa,
}

func BenchmarkTxVerifier(b *testing.B) {
	config := baseConfig
	for _, ratio := range []test.Percentage{0.5, test.Always} {
		config.InputGeneratorParams.ValidSigRatio = ratio
		b.Run(fmt.Sprintf("%s-r%f", config.Name, ratio), func(b *testing.B) {
			g := NewInputGenerator(config.InputGeneratorParams)
			factory := sigverification_test.GetSignatureFactory(config.VerificationScheme)
			privateKey, publicKey := factory.NewKeys()
			txSigner, _ := factory.NewSigner(privateKey)
			txVerifier, _ := factory.NewVerifier(publicKey)

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				b.StopTimer()
				tx := &token.Tx{SerialNumbers: g.NextTxInput()}
				isValid := g.NextValid()
				if isValid {
					tx.Signature, _ = txSigner.SignTx(tx.SerialNumbers)
				}
				b.StartTimer()

				txVerifier.VerifyTx(tx)
			}
		})
	}
}

// Input generator

type inputGeneratorParams struct {
	*sigverification_test.TxInputGeneratorParams
	ValidSigRatio test.Percentage
}
type inputGenerator struct {
	txInputGenerator *sigverification_test.TxInputGenerator
	validGenerator   *test.BooleanGenerator
}

func NewInputGenerator(params *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		txInputGenerator: sigverification_test.NewTxInputGenerator(params.TxInputGeneratorParams),
		validGenerator:   test.NewBooleanGenerator(test.PercentageUniformDistribution, params.ValidSigRatio, 30),
	}
}

func (g *inputGenerator) NextTxInput() []signature.SerialNumber {
	return g.txInputGenerator.Next()
}

func (g *inputGenerator) NextValid() bool {
	return g.validGenerator.Next()
}
