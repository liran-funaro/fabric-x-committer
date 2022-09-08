package parallelexecutor_test

import (
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/testutils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

type benchmarkConfig struct {
	Name                    string
	ParallelExecutionConfig parallelexecutor.Config
	InputGeneratorParams    inputGeneratorParams
}

var baseConfig = benchmarkConfig{
	Name: "basic",
	ParallelExecutionConfig: parallelexecutor.Config{
		BatchSizeCutoff:   100,
		BatchTimeCutoff:   1 * time.Second,
		Parallelism:       6,
		ChannelBufferSize: 1,
	},
	InputGeneratorParams: inputGeneratorParams{
		InputDelay:    test.NoDelay,
		BatchSize:     test.Constant(100),
		ExecutorDelay: test.Stable(int64(time.Second / 10_000)),
	},
}

func BenchmarkParallelExecutor(b *testing.B) {
	var output = test.Open("results.txt", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Parallelism", Formatter: test.NoFormatting},
		{Header: "Batch size", Formatter: test.ConstantDistributionFormatter},
		{Header: "Throughput", Formatter: test.NoFormatting},
		{Header: "Memory", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats testutils.AsyncTrackerStats
	var iConfig benchmarkConfig
	for i := test.NewBenchmarkIterator(baseConfig, "ParallelExecutionConfig.Parallelism", 2, 4); i.HasNext(); i.Next() {
		i.Read(&iConfig)
		var jConfig benchmarkConfig
		for j := test.NewBenchmarkIterator(iConfig, "InputGeneratorParams.BatchSize", test.Constant(200), test.Constant(300)); j.HasNext(); j.Next() {
			j.Read(&jConfig)
			b.Run(jConfig.Name, func(b *testing.B) {
				g := NewInputGenerator(&jConfig.InputGeneratorParams)
				e := parallelexecutor.New(g.Executor(), &jConfig.ParallelExecutionConfig)
				t := testutils.NewAsyncTracker(testutils.NoSampling)

				t.Start(e.Outputs())
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						batch := g.NextRequestBatch()
						t.SubmitRequests(len(batch.Requests))

						e.Submit(batch.Requests)
					}
				})
				stats = t.WaitUntilDone()
			})
			output.Record(jConfig.ParallelExecutionConfig.Parallelism, jConfig.InputGeneratorParams.BatchSize, stats.RequestsPer(time.Second), stats.TotalMemory)
		}
	}
}

// Input generator

type inputGeneratorParams struct {
	InputDelay, BatchSize, ExecutorDelay test.Distribution
}

func NewInputGenerator(params *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		inputDelayGenerator:    test.NewDelayGenerator(params.InputDelay, 30),
		requestBatchGenerator:  testutils.NewEmptyRequestBatchGenerator(params.BatchSize),
		executorDelayGenerator: test.NewDelayGenerator(params.ExecutorDelay, 30),
	}
}

type inputGenerator struct {
	inputDelayGenerator    *test.DelayGenerator
	requestBatchGenerator  *testutils.EmptyRequestBatchGenerator
	executorDelayGenerator *test.DelayGenerator
}

func (c *inputGenerator) NextRequestBatch() *sigverification.RequestBatch {
	c.inputDelayGenerator.Next()
	return &sigverification.RequestBatch{Requests: c.requestBatchGenerator.Next()}
}

func (c *inputGenerator) Executor() parallelexecutor.ExecutorFunc {
	return func(input *parallelexecutor.Input) (*parallelexecutor.Output, error) {
		c.executorDelayGenerator.Next()
		return &parallelexecutor.Output{}, nil
	}
}
