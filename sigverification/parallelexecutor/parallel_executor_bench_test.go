package parallelexecutor_test

import (
	"fmt"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	_ "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/performance"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
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
		ExecutorDelay: test.Constant(int64(time.Second / 10_000)),
	},
}

func BenchmarkParallelExecutor(b *testing.B) {
	var output = test.Open("pe", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Parallelism", Formatter: test.NoFormatting},
		{Header: "Batch size", Formatter: test.ConstantDistributionFormatter},
		{Header: "Throughput", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats sigverification_test.AsyncTrackerStats
	config := baseConfig
	for _, parallelism := range []int{4} {
		config.ParallelExecutionConfig.Parallelism = parallelism
		for _, batchSize := range []int64{100} {
			config.InputGeneratorParams.BatchSize = test.Constant(batchSize)
			b.Run(fmt.Sprintf("%s-p%d-b%d", config.Name, parallelism, batchSize), func(b *testing.B) {
				g := NewInputGenerator(&config.InputGeneratorParams)
				e := parallelexecutor.New(g.Executor(), &config.ParallelExecutionConfig)
				t := sigverification_test.NewAsyncTracker()

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
				b.StopTimer()
			})
			output.Record(config.ParallelExecutionConfig.Parallelism, config.InputGeneratorParams.BatchSize, stats.RequestsPer(time.Second))
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
		requestBatchGenerator:  sigverification_test.NewEmptyRequestBatchGenerator(params.BatchSize),
		executorDelayGenerator: test.NewDelayGenerator(params.ExecutorDelay, 30),
	}
}

type inputGenerator struct {
	inputDelayGenerator    *test.DelayGenerator
	requestBatchGenerator  *sigverification_test.EmptyRequestBatchGenerator
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
