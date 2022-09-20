package verifierserver_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

type benchmarkConfig struct {
	Name                    string
	ParallelExecutionConfig *parallelexecutor.Config
	InputGeneratorParams    *sigverification_test.InputGeneratorParams
}

var privateKey, publicKey = sigverification_test.GetSignatureFactory(sigverification_test.VerificationScheme).NewKeys()

var baseConfig = benchmarkConfig{
	Name: "basic",
	ParallelExecutionConfig: &parallelexecutor.Config{
		BatchSizeCutoff:   sigverification_test.BatchSize,
		BatchTimeCutoff:   sigverification_test.OptimalBatchTimeCutoff,
		Parallelism:       4,
		ChannelBufferSize: sigverification_test.OptimalChannelBufferSize,
	},
	InputGeneratorParams: &sigverification_test.InputGeneratorParams{
		InputDelay: test.NoDelay,
		RequestBatch: sigverification_test.RequestBatchGeneratorParams{
			Tx: sigverification_test.TxGeneratorParams{
				SigningKey:       privateKey,
				Scheme:           sigverification_test.VerificationScheme,
				ValidSigRatio:    sigverification_test.SignatureValidRatio,
				TxSize:           sigverification_test.TxSize,
				SerialNumberSize: sigverification_test.SerialNumberSize,
			},
			BatchSize: sigverification_test.BatchSizeDistribution,
		},
	},
}

func BenchmarkVerifierServer(b *testing.B) {
	var output = test.Open("server", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Parallelism", Formatter: test.NoFormatting},
		{Header: "Batch size", Formatter: test.ConstantDistributionFormatter},
		{Header: "Throughput", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats sigverification_test.AsyncTrackerStats
	config := baseConfig
	for _, parallelism := range []int{1, 4, 8, 16, 32, 40, 64, 80} {
		config.ParallelExecutionConfig.Parallelism = parallelism
		for _, batchSize := range []int64{50, 100} {
			config.InputGeneratorParams.RequestBatch.BatchSize = test.Constant(batchSize)
			b.Run(fmt.Sprintf("%s-p%d-b%v", config.Name, config.ParallelExecutionConfig.Parallelism, test.ConstantDistributionFormatter(config.InputGeneratorParams.RequestBatch.BatchSize)), func(b *testing.B) {
				g := sigverification_test.NewInputGenerator(config.InputGeneratorParams)
				server := verifierserver.New(config.ParallelExecutionConfig, config.InputGeneratorParams.RequestBatch.Tx.Scheme)
				c := sigverification_test.NewTestState(server)
				t := sigverification_test.NewAsyncTracker()
				defer c.TearDown()
				c.Client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: publicKey})
				stream, _ := c.Client.StartStream(context.Background())
				send := sigverification_test.InputChannel(stream)

				t.Start(sigverification_test.OutputChannel(stream))
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						batch := g.NextRequestBatch()
						t.SubmitRequests(len(batch.Requests))

						send <- batch
					}
				})
				stats = t.WaitUntilDone()
				b.StopTimer()
			})
			output.Record(config.ParallelExecutionConfig.Parallelism, config.InputGeneratorParams.RequestBatch.BatchSize, stats.RequestsPer(time.Second))
		}
	}
}
