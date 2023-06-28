package parallelexecutor_test

import (
	"fmt"
	"testing"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/sigverification"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type benchmarkConfig struct {
	Name                    string
	ParallelExecutionConfig parallelexecutor.Config
	InputGeneratorParams    inputGeneratorParams
}

var baseConfig = benchmarkConfig{
	Name: "basic",
	ParallelExecutionConfig: parallelexecutor.Config{
		BatchSizeCutoff:   test.BatchSize,
		BatchTimeCutoff:   sigverification_test.OptimalBatchTimeCutoff,
		Parallelism:       6,
		ChannelBufferSize: sigverification_test.OptimalChannelBufferSize,
	},
	InputGeneratorParams: inputGeneratorParams{
		InputDelay:    test.ClientInputDelay,
		BatchSize:     sigverification_test.BatchSizeDistribution,
		ExecutorDelay: test.Constant(sigverification_test.TypicalTxValidationDelay),
	},
}

func BenchmarkParallelExecutor(b *testing.B) {
	var output = test.Open("pe", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Parallelism", Formatter: test.NoFormatting},
		{Header: "Batch size", Formatter: test.ConstantDistributionFormatter},
		{Header: "Throughput", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats connection.RequestTrackerStats
	config := baseConfig
	for _, parallelism := range []int{1, 4, 8, 16, 32, 40, 64, 80} {
		config.ParallelExecutionConfig.Parallelism = parallelism
		for _, batchSize := range []int64{100} {
			config.InputGeneratorParams.BatchSize = test.Constant(batchSize)
			b.Run(fmt.Sprintf("%s-p%d-b%d", config.Name, parallelism, batchSize), func(b *testing.B) {
				g := NewInputGenerator(&config.InputGeneratorParams)
				m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
				e := parallelexecutor.New(g.Executor(), &config.ParallelExecutionConfig, m)
				t := connection.NewRequestTracker()

				t.StartWithOutputReceived(sigverification_test.ChannelOutputLength(e.Outputs()))
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						batch := g.NextRequestBatch()
						t.SubmitRequests(len(batch.Requests))

						e.Submit(batch.Requests)
					}
				})
				t.WaitUntilDone()
				stats = *t.CurrentStats()
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
