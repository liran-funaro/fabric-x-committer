package signature_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

var baseInputGeneratorParams = &inputGeneratorParams{
	SomeTxInputGeneratorParams: &sigverification_test.SomeTxInputGeneratorParams{
		TxSize:           sigverification_test.SerialNumberCountDistribution,
		SerialNumberSize: sigverification_test.SerialNumberSize,
	},
	ValidSigRatio: sigverification_test.SignatureValidRatio,
}

var ecdsaConfig = benchmarkConfig{
	Name:                 "ecdsa",
	InputGeneratorParams: baseInputGeneratorParams,
	VerificationScheme:   signature.Ecdsa,
}

var blsConfig = benchmarkConfig{
	Name:                 "bls",
	InputGeneratorParams: baseInputGeneratorParams,
	VerificationScheme:   signature.Bls,
}

var eddsaConfig = benchmarkConfig{
	Name:                 "eddsa",
	InputGeneratorParams: baseInputGeneratorParams,
	VerificationScheme:   signature.Eddsa,
}

// go test -benchmem -bench Bench -run=^$ -cpu=1
func BenchmarkTxVerifier(b *testing.B) {
	configs := []benchmarkConfig{ecdsaConfig, eddsaConfig, blsConfig}
	for _, config := range configs {
		b.Run(config.Name, func(b *testing.B) {
			g := NewInputGenerator(config.InputGeneratorParams)
			factory := sigverification_test.GetSignatureFactory(config.VerificationScheme)
			privateKey, publicKey := factory.NewKeys()
			txSigner, err := factory.NewSigner(privateKey)
			assert.NoError(b, err)
			txVerifier, err := factory.NewVerifier(publicKey)
			assert.NoError(b, err)

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				b.StopTimer()

				// create tx with a single namespace
				tx := &protoblocktx.Tx{
					Namespaces: []*protoblocktx.TxNamespace{{NsId: 1}},
				}

				// attach all inputs (sns) as readWrites
				for _, sn := range g.NextTxInput() {
					tx.Namespaces[0].ReadWrites = append(tx.Namespaces[0].GetReadWrites(), &protoblocktx.ReadWrite{
						Key: sn,
					})
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
	}
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
