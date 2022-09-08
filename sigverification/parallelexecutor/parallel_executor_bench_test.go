package parallelexecutor_test

import (
	"log"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/testutils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

var benchmarkConfigs = []struct {
	name                    string
	parallelExecutionConfig *parallelexecutor.Config
	inputGeneratorParams    *inputGeneratorParams
}{{
	name: "basic",
	parallelExecutionConfig: &parallelexecutor.Config{
		BatchSizeCutoff:   30,
		BatchTimeCutoff:   1 * time.Second,
		Parallelism:       3,
		ChannelBufferSize: 1,
	},
	inputGeneratorParams: &inputGeneratorParams{
		inputDelay:    test.Volatile(int64(1 * time.Millisecond)),
		batchSize:     test.Constant(100),
		executorDelay: test.Stable(int64(150 * time.Microsecond)),
	},
}}

func BenchmarkParallelExecutor(b *testing.B) {
	for _, config := range benchmarkConfigs {
		b.Run(config.name, func(b *testing.B) {
			g := NewInputGenerator(config.inputGeneratorParams)
			e := parallelexecutor.New(g.Executor(), config.parallelExecutionConfig)

			requestsSent, wait := testutils.Track(e.Outputs())
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					batch := g.NextRequestBatch()
					requestsSent(len(batch.Requests))
					e.Submit(batch.Requests)
				}
			})
			rate := wait()
			log.Printf("Rate: %d Reqs/sec for config %s", rate, config.name)
		})
	}

}

// Input generator

type inputGeneratorParams struct {
	inputDelay, batchSize, executorDelay test.Distribution
}

func NewInputGenerator(params *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		inputDelayGenerator:    test.NewDelayGenerator(params.inputDelay, 30),
		requestBatchGenerator:  testutils.NewEmptyRequestBatchGenerator(params.batchSize),
		executorDelayGenerator: test.NewDelayGenerator(params.executorDelay, 30),
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
