package signature_test

import (
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
		SomeTxInputGeneratorParams: &sigverification_test.SomeTxInputGeneratorParams{
			TxSize:           sigverification_test.TxSizeDistribution,
			SerialNumberSize: sigverification_test.SerialNumberSize,
		},
		ValidSigRatio: sigverification_test.SignatureValidRatio,
	},
	VerificationScheme: sigverification_test.VerificationScheme,
}

func BenchmarkTxVerifier(b *testing.B) {
	config := baseConfig
	//for _, ratio := range []test.Percentage{0.5, test.Always} {
	//	config.InputGeneratorParams.ValidSigRatio = ratio
	b.Run(config.Name, func(b *testing.B) {
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
	//}
}

// Input generator

type inputGeneratorParams struct {
	*sigverification_test.SomeTxInputGeneratorParams
	ValidSigRatio test.Percentage
}
type inputGenerator struct {
	txInputGenerator *sigverification_test.SomeTxInputGenerator
	validGenerator   *test.BooleanGenerator
}

func NewInputGenerator(params *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		txInputGenerator: sigverification_test.NewSomeTxInputGenerator(params.SomeTxInputGeneratorParams),
		validGenerator:   test.NewBooleanGenerator(test.PercentageUniformDistribution, params.ValidSigRatio, 30),
	}
}

func (g *inputGenerator) NextTxInput() []token.SerialNumber {
	return g.txInputGenerator.Next()
}

func (g *inputGenerator) NextValid() bool {
	return g.validGenerator.Next()
}
