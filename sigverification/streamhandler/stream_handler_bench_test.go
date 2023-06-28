package streamhandler_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/sigverification"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type benchmarkConfig struct {
	Name                 string
	InputGeneratorParams *inputGeneratorParams
}

var baseConfig = benchmarkConfig{
	Name: "basic",
	InputGeneratorParams: &inputGeneratorParams{
		BatchSize:   sigverification_test.BatchSizeDistribution,
		ClientDelay: test.ClientInputDelay,
		ServerDelay: test.Constant(int64(test.BatchSize) * sigverification_test.TypicalTxValidationDelay),
	},
}

func BenchmarkStreamHandler(b *testing.B) {
	var output = test.Open("stream", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Batch size", Formatter: test.ConstantDistributionFormatter},
		{Header: "Server delay", Formatter: test.ConstantDistributionFormatter},
		{Header: "Throughput", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats connection.RequestTrackerStats
	config := baseConfig
	for _, batchSize := range []int64{50, 100} {
		config.InputGeneratorParams.BatchSize = test.Constant(batchSize)
		for _, serverDelay := range []int64{0, batchSize * sigverification_test.TypicalTxValidationDelay} {
			config.InputGeneratorParams.ServerDelay = test.Constant(serverDelay)
			b.Run(fmt.Sprintf("%s-b%d-d%v", config.Name, batchSize, time.Duration(serverDelay)), func(b *testing.B) {
				g := NewInputGenerator(config.InputGeneratorParams)
				s := sigverification_test.NewTestState(g.VerifierServer())
				t := connection.NewRequestTracker()
				defer s.TearDown()
				stream, _ := s.Client.StartStream(context.Background())
				send := sigverification_test.InputChannel(stream)

				t.StartWithOutputReceived(sigverification_test.StreamOutputLength(stream))
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						batch := g.NextRequestBatch()
						t.SubmitRequests(len(batch.Requests))

						send <- batch
					}
				})
				t.WaitUntilDone()
				stats = *t.CurrentStats()
			})
			output.Record(config.InputGeneratorParams.BatchSize, config.InputGeneratorParams.ServerDelay, stats.RequestsPer(time.Second))
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
	privateKey, _ := sigverification_test.GetSignatureFactory(sigverification_test.VerificationScheme).NewKeys()
	batchGen := sigverification_test.NewRequestBatchGenerator(&sigverification_test.RequestBatchGeneratorParams{
		Tx: sigverification_test.TxGeneratorParams{
			SigningKey:        privateKey,
			Scheme:            sigverification_test.VerificationScheme,
			ValidSigRatio:     sigverification_test.SignatureValidRatio,
			SerialNumberCount: sigverification_test.SerialNumberCountDistribution,
			OutputCount:       sigverification_test.OutputCountDistribution,
			SerialNumberSize:  sigverification_test.SerialNumberSize,
			OutputSize:        sigverification_test.OutputSize,
		},
		BatchSize: p.BatchSize,
	}, 100)
	return &inputGenerator{
		verifierServer:       sigverification_test.NewDummyVerifierServer(p.ServerDelay),
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
