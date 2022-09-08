package streamhandler_test

import (
	"context"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/testutils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

type benchmarkConfig struct {
	Name                 string
	InputGeneratorParams *inputGeneratorParams
}

var baseConfig = benchmarkConfig{
	Name: "basic",
	InputGeneratorParams: &inputGeneratorParams{
		ClientDelay: test.NoDelay,
		ServerDelay: test.Stable(int64(time.Second / 635)),
	},
}

func BenchmarkStreamHandler(b *testing.B) {
	var output = test.Open("results.txt", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Batch size", Formatter: test.ConstantDistributionFormatter},
		{Header: "Throughput", Formatter: test.NoFormatting},
		{Header: "Memory", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats testutils.AsyncTrackerStats
	var iConfig benchmarkConfig
	for i := test.NewBenchmarkIterator(baseConfig, "InputGeneratorParams.ServerDelay", test.Constant(int64(time.Second/635))); i.HasNext(); i.Next() {
		i.Read(&iConfig)
		b.Run(iConfig.Name, func(b *testing.B) {
			g := NewInputGenerator(iConfig.InputGeneratorParams)
			s := testutils.NewTestState(g.VerifierServer())
			t := testutils.NewAsyncTracker(testutils.NoSampling)
			defer s.TearDown()
			stream, _ := s.Client.StartStream(context.Background())
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
		output.Record(iConfig.InputGeneratorParams.ServerDelay, stats.RequestsPer(time.Second), stats.TotalMemory)
	}
}

// Input generator

type inputGeneratorParams struct {
	ClientDelay test.Distribution
	ServerDelay test.Distribution
}
type inputGenerator struct {
	verifierServer       sigverification.VerifierServer
	clientDelayGenerator *test.DelayGenerator
	requestBatch         *sigverification.RequestBatch
}

func NewInputGenerator(p *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		verifierServer:       testutils.NewDummyVerifierServer(p.ServerDelay),
		requestBatch:         &sigverification.RequestBatch{Requests: []*sigverification.Request{{}}},
		clientDelayGenerator: test.NewDelayGenerator(p.ClientDelay, 30),
	}
}

func (c *inputGenerator) NextRequestBatch() *sigverification.RequestBatch {
	return c.requestBatch
}

func (c *inputGenerator) VerifierServer() sigverification.VerifierServer {
	return c.verifierServer
}
