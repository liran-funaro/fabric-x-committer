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
}

var baseConfig = benchmarkConfig{
	Name: "basic",
	InputGeneratorParams: &inputGeneratorParams{&testutils.TxGeneratorParams{
		Scheme:           signature.Ecdsa,
		ValidSigRatio:    test.Always,
		TxSize:           test.Constant(1),
		SerialNumberSize: test.Constant(64),
	}},
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
			txVerifier := signature.NewTxVerifier(iConfig.InputGeneratorParams.Scheme)
			publicKey := g.PublicKey()

			t.Start()
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				b.StopTimer()
				tx := g.NextTx()
				b.StartTimer()

				txVerifier.VerifyTx(publicKey, tx)
			}
			totalSigs = b.N
			stats = t.Stop()
		})
		output.Record(iConfig.InputGeneratorParams.ValidSigRatio, float64(totalSigs)*float64(time.Second)/float64(stats.TotalTime), stats.TotalMemory)
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
