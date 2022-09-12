package streamhandler_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
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
		BatchSize:   test.Constant(100),
		ClientDelay: test.NoDelay,
		ServerDelay: test.Stable(int64(time.Second / 635)),
	},
}

func BenchmarkStreamHandler(b *testing.B) {
	var output = test.Open("stream", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Batch size", Formatter: test.ConstantDistributionFormatter},
		{Header: "Server delay", Formatter: test.ConstantDistributionFormatter},
		{Header: "Throughput", Formatter: test.NoFormatting},
		{Header: "Memory", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats testutils.AsyncTrackerStats
	config := baseConfig
	for _, batchSize := range []int64{10, 20, 30, 40, 50, 100, 200} {
		config.InputGeneratorParams.BatchSize = test.Constant(batchSize)
		for _, serverDelay := range []int64{0, int64(time.Second) * batchSize / 20_000} {
			config.InputGeneratorParams.ServerDelay = test.Constant(serverDelay)
			b.Run(fmt.Sprintf("%s-b%d-d%v", config.Name, batchSize, time.Duration(serverDelay)), func(b *testing.B) {
				g := NewInputGenerator(config.InputGeneratorParams)
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
			output.Record(config.InputGeneratorParams.BatchSize, config.InputGeneratorParams.ServerDelay, stats.RequestsPer(time.Second), stats.TotalMemory)
		}
	}
}

// Input generator

type inputGeneratorParams struct {
	BatchSize   test.Distribution
	ClientDelay test.Distribution
	ServerDelay test.Distribution
}
type inputGenerator struct {
	verifierServer       sigverification.VerifierServer
	clientDelayGenerator *test.DelayGenerator
	requestBatch         *sigverification.RequestBatch
}

func NewInputGenerator(p *inputGeneratorParams) *inputGenerator {
	batchGen := testutils.NewRequestBatchGenerator(&testutils.RequestBatchGeneratorParams{
		Tx: &testutils.TxGeneratorParams{
			Scheme:           signature.Ecdsa,
			ValidSigRatio:    test.Always,
			TxSize:           test.Constant(1),
			SerialNumberSize: test.Constant(64),
		},
		BatchSize: p.BatchSize,
	}, 100)
	return &inputGenerator{
		verifierServer:       testutils.NewDummyVerifierServer(p.ServerDelay),
		requestBatch:         &sigverification.RequestBatch{Requests: batchGen.Next()},
		clientDelayGenerator: test.NewDelayGenerator(p.ClientDelay, 30),
	}
}

func (c *inputGenerator) NextRequestBatch() *sigverification.RequestBatch {
	return c.requestBatch
}

func (c *inputGenerator) VerifierServer() sigverification.VerifierServer {
	return c.verifierServer
}
