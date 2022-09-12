package signature_test

import (
	"fmt"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/testutils"
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
		TxInputGeneratorParams: &testutils.TxInputGeneratorParams{
			TxSize:           test.Constant(1),
			SerialNumberSize: test.Constant(64),
		},
		ValidSigRatio: test.Always,
	},
	VerificationScheme: signature.Ecdsa,
}

func BenchmarkTxVerifier(b *testing.B) {
	var output = test.Open("signature", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Valid Sig Ratio", Formatter: test.NoFormatting},
		{Header: "Throughput", Formatter: test.NoFormatting},
		{Header: "Memory", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats testutils.SyncTrackerStats
	config := baseConfig
	for _, ratio := range []test.Percentage{test.Never, 0.5, test.Always} {
		config.InputGeneratorParams.ValidSigRatio = ratio
		totalSigs := 0
		b.Run(fmt.Sprintf("%s-r%f", config.Name, ratio), func(b *testing.B) {
			g := NewInputGenerator(config.InputGeneratorParams)
			t := testutils.NewSyncTracker(1 * time.Millisecond)
			txSigner, txVerifier := signature.NewSignerVerifier(config.VerificationScheme)

			t.Start()
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
			totalSigs = b.N
			stats = t.Stop()
		})
		output.Record(config.InputGeneratorParams.ValidSigRatio, float64(totalSigs)*float64(time.Second)/float64(stats.TotalTime), stats.TotalMemory)
	}
}

// Input generator

type inputGeneratorParams struct {
	*testutils.TxInputGeneratorParams
	ValidSigRatio test.Percentage
}
type inputGenerator struct {
	txInputGenerator *testutils.TxInputGenerator
	validGenerator   *test.BooleanGenerator
}

func NewInputGenerator(params *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		txInputGenerator: testutils.NewTxInputGenerator(params.TxInputGeneratorParams),
		validGenerator:   test.NewBooleanGenerator(test.PercentageUniformDistribution, params.ValidSigRatio, 30),
	}
}

func (g *inputGenerator) NextTxInput() []signature.SerialNumber {
	return g.txInputGenerator.Next()
}

func (g *inputGenerator) NextValid() bool {
	return g.validGenerator.Next()
}
