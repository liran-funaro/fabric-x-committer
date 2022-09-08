package streamhandler_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/testutils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

var benchmarkConfigs = []struct {
	name                 string
	inputGeneratorParams *inputGeneratorParams
}{{
	name: "basic",
	inputGeneratorParams: &inputGeneratorParams{
		requestBatchSize: test.Constant(1),
		clientDelay:      test.Stable(int64(10 * time.Microsecond)),
		serverDelay:      test.NoDelay,
	},
}}

func BenchmarkStreamHandler(b *testing.B) {
	for _, config := range benchmarkConfigs {
		b.Run(config.name, func(b *testing.B) {
			g := NewInputGenerator(config.inputGeneratorParams)
			s := testutils.NewTestState(g.VerifierServer())
			defer s.TearDown()
			stream, _ := s.Client.StartStream(context.Background())
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
			log.Printf("Rate: %d Reqs (batches)/sec for config %s", rate, config.name)
		})
	}

}

// Input generator

type inputGeneratorParams struct {
	requestBatchSize test.Distribution
	clientDelay      test.Distribution
	serverDelay      test.Distribution
}
type inputGenerator struct {
	verifierServer        sigverification.VerifierServer
	requestBatchGenerator *testutils.EmptyRequestBatchGenerator
	clientDelayGenerator  *test.DelayGenerator
}

func NewInputGenerator(p *inputGeneratorParams) *inputGenerator {
	return &inputGenerator{
		verifierServer:        testutils.NewDummyVerifierServer(p.serverDelay),
		requestBatchGenerator: testutils.NewEmptyRequestBatchGenerator(p.requestBatchSize),
		clientDelayGenerator:  test.NewDelayGenerator(p.clientDelay, 30),
	}
}

func (c *inputGenerator) NextRequestBatch() *sigverification.RequestBatch {
	c.clientDelayGenerator.Next()
	return &sigverification.RequestBatch{Requests: c.requestBatchGenerator.Next()}
}

func (c *inputGenerator) VerifierServer() sigverification.VerifierServer {
	return c.verifierServer
}
