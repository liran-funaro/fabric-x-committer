package signature_test

import (
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
	var output = test.Open("results.txt", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Valid Sig Ratio", Formatter: test.NoFormatting},
		{Header: "Throughput", Formatter: test.NoFormatting},
		{Header: "Memory", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats testutils.SyncTrackerStats
	var iConfig benchmarkConfig
	for i := test.NewBenchmarkIterator(baseConfig, "InputGeneratorParams.ValidSigRatio", test.Never, 0.5, test.Always); i.HasNext(); i.Next() {
		i.Read(&iConfig)
		totalSigs := 0
		b.Run(iConfig.Name, func(b *testing.B) {
			g := NewInputGenerator(iConfig.InputGeneratorParams)
			t := testutils.NewSyncTracker(1 * time.Millisecond)
			txSigner, txVerifier := signature.NewSignerVerifier(iConfig.VerificationScheme)

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
		output.Record(iConfig.InputGeneratorParams.ValidSigRatio, float64(totalSigs)*float64(time.Second)/float64(stats.TotalTime), stats.TotalMemory)
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
