package verifierserver_test

import (
	"context"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/testutils"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

type benchmarkConfig struct {
	Name                    string
	ParallelExecutionConfig *parallelexecutor.Config
	InputGeneratorParams    *inputGeneratorParams
}

var baseConfig = benchmarkConfig{
	Name: "basic",
	ParallelExecutionConfig: &parallelexecutor.Config{
		BatchSizeCutoff:   100,
		BatchTimeCutoff:   1 * time.Second,
		Parallelism:       3,
		ChannelBufferSize: 1,
	},
	InputGeneratorParams: &inputGeneratorParams{
		InputDelay: test.Stable(int64(10 * time.Microsecond)),
		RequestBatch: &testutils.RequestBatchGeneratorParams{
			Tx: &testutils.TxGeneratorParams{
				Scheme:           signature.Ecdsa,
				ValidSigRatio:    0.8,
				TxSize:           test.Stable(20),
				SerialNumberSize: test.Constant(10),
			},
			BatchSize: test.Constant(100),
		},
	},
}

func BenchmarkVerifierServer(b *testing.B) {
	var output = test.Open("results.txt", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Parallelism", Formatter: test.NoFormatting},
		{Header: "Throughput", Formatter: test.NoFormatting},
		{Header: "Memory", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats testutils.AsyncTrackerStats
	var iConfig benchmarkConfig
	for i := test.NewBenchmarkIterator(baseConfig, "ParallelExecutionConfig.Parallelism", 2, 4); i.HasNext(); i.Next() {
		i.Read(&iConfig)
		b.Run(iConfig.Name, func(b *testing.B) {
			g := NewInputGenerator(iConfig.InputGeneratorParams)
			c := testutils.NewTestState(verifierserver.New(iConfig.ParallelExecutionConfig, iConfig.InputGeneratorParams.RequestBatch.Tx.Scheme))
			t := testutils.NewAsyncTracker(testutils.NoSampling)
			defer c.TearDown()
			c.Client.SetVerificationKey(context.Background(), g.PublicKey())
			stream, _ := c.Client.StartStream(context.Background())
			send := testutils.InputChannel(stream)

			t.Start(testutils.OutputChannel(stream))
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					batch := g.NextRequestBatch()
					t.SubmitRequests(len(batch.Requests))

					send <- batch
				}
			})
			stats = t.WaitUntilDone()
		})
		output.Record(iConfig.ParallelExecutionConfig.Parallelism, stats.RequestsPer(time.Second), stats.TotalMemory)
	}

}

// Input generator

type inputGeneratorParams struct {
	InputDelay   test.Distribution
	RequestBatch *testutils.RequestBatchGeneratorParams
}
type inputGenerator struct {
	inputDelayGenerator   *test.DelayGenerator
	requestBatchGenerator *testutils.RequestBatchGenerator
}

func NewInputGenerator(p *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		inputDelayGenerator:   test.NewDelayGenerator(p.InputDelay, 30),
		requestBatchGenerator: testutils.NewRequestBatchGenerator(p.RequestBatch, 30),
	}
}

func (c *inputGenerator) NextRequestBatch() *sigverification.RequestBatch {
	c.inputDelayGenerator.Next()
	return &sigverification.RequestBatch{Requests: c.requestBatchGenerator.Next()}
}

func (c *inputGenerator) PublicKey() *sigverification.Key {
	return &sigverification.Key{SerializedBytes: c.requestBatchGenerator.PublicKey}
}
