package verifierserver_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
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
		BatchSizeCutoff:   100,
		BatchTimeCutoff:   1 * time.Second,
		Parallelism:       3,
		ChannelBufferSize: 1,
	},
	inputGeneratorParams: &inputGeneratorParams{
		inputDelay: test.Stable(int64(10 * time.Microsecond)),
		requestBatch: &testutils.RequestBatchGeneratorParams{
			Tx: &testutils.TxGeneratorParams{
				Scheme:           signature.Ecdsa,
				ValidSigRatio:    0.8,
				TxSize:           test.Stable(20),
				SerialNumberSize: test.Constant(10),
			},
			BatchSize: test.Constant(100),
		},
	},
}}

func BenchmarkVerifierServer(b *testing.B) {
	for _, config := range benchmarkConfigs {
		b.Run(config.name, func(b *testing.B) {
			g := NewInputGenerator(config.inputGeneratorParams)
			c := &testState{parallelExecutionConfig: config.parallelExecutionConfig}
			defer c.tearDown()
			c.setUp(config.inputGeneratorParams.requestBatch.Tx.Scheme)
			c.client.SetVerificationKey(context.Background(), g.PublicKey())
			stream, _ := c.client.StartStream(context.Background())
			send := inputChannel(stream)

			requestsSent, wait := testutils.Track(outputChannel(stream))
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					g.NextInputDelay()
					batch := g.NextRequestBatch()
					send <- batch
					requestsSent(len(batch.Requests))
				}
			})
			rate := wait()
			log.Printf("Rate: %d TX/sec for config %s", rate, config.name)
		})
	}

}

// Input generator

type inputGeneratorParams struct {
	inputDelay   *test.NormalDistribution
	requestBatch *testutils.RequestBatchGeneratorParams
}
type inputGenerator struct {
	inputDelayGenerator   *test.DelayGenerator
	requestBatchGenerator *testutils.RequestBatchGenerator
}

func NewInputGenerator(p *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		inputDelayGenerator:   test.NewDelayGenerator(p.inputDelay, 30),
		requestBatchGenerator: testutils.NewRequestBatchGenerator(p.requestBatch, 30),
	}
}

func (c *inputGenerator) NextRequestBatch() *sigverification.RequestBatch {
	return &sigverification.RequestBatch{Requests: c.requestBatchGenerator.Next()}
}

func (c *inputGenerator) NextInputDelay() {
	c.inputDelayGenerator.Next()
}

func (c *inputGenerator) PublicKey() *sigverification.Key {
	return &sigverification.Key{SerializedBytes: c.requestBatchGenerator.PublicKey}
}
