package main

import (
	"testing"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	tokenutil "github.ibm.com/decentralized-trust-research/scalable-committer/utils/token"
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
			TxSize:           sigverification_test.SerialNumberCountDistribution,
			SerialNumberSize: sigverification_test.SerialNumberSize,
		},
		ValidSigRatio: sigverification_test.SignatureValidRatio,
	},
	VerificationScheme: sigverification_test.VerificationScheme,
}

func BenchmarkTxVerifier(b *testing.B) {
	config := baseConfig
	// for _, ratio := range []test.Percentage{0.5, test.Always} {
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

			tx := &protoblocktx.Tx{
				Namespaces: []*protoblocktx.TxNamespace{{
					NsId:       0,
					ReadWrites: []*protoblocktx.ReadWrite{},
				}},
			}
			for _, sn := range g.NextTxInput() {
				tx.Namespaces[0].ReadWrites = append(tx.Namespaces[0].ReadWrites, &protoblocktx.ReadWrite{Key: sn})
			}

			isValid := g.NextValid()
			if isValid {
				s, _ := txSigner.SignNs(tx, 0)
				tx.Signatures = append(tx.Signatures, s)
			}
			b.StartTimer()

			txVerifier.VerifyNs(tx, 0)
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

func (g *inputGenerator) NextTxInput() []tokenutil.SerialNumber {
	return g.txInputGenerator.Next()
}

func (g *inputGenerator) NextValid() bool {
	return g.validGenerator.Next()
}

func main() {
	var tests []testing.InternalBenchmark

	tests = append(tests, testing.InternalBenchmark{
		Name: "Verify",
		F:    BenchmarkTxVerifier,
	})

	testing.MainStart(nil, nil, tests, nil, nil)
}
