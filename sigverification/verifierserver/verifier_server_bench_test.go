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
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
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
			c := testutils.NewTestState(verifierserver.New(config.parallelExecutionConfig, config.inputGeneratorParams.requestBatch.Tx.Scheme))
			defer c.TearDown()
			c.Client.SetVerificationKey(context.Background(), g.PublicKey())
			stream, _ := c.Client.StartStream(context.Background())
			send := testutils.InputChannel(stream)

			requestsSent, wait := testutils.Track(testutils.OutputChannel(stream))
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					batch := g.NextRequestBatch()
					requestsSent(len(batch.Requests))
					send <- batch
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
	c.inputDelayGenerator.Next()
	return &sigverification.RequestBatch{Requests: c.requestBatchGenerator.Next()}
}

func (c *inputGenerator) PublicKey() *sigverification.Key {
	return &sigverification.Key{SerializedBytes: c.requestBatchGenerator.PublicKey}
}
