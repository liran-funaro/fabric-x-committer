package verifierserver_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

type benchmarkConfig struct {
	Name                    string
	ParallelExecutionConfig *parallelexecutor.Config
	InputGeneratorParams    *sigverification_test.InputGeneratorParams
}

var privateKey, publicKey = sigverification_test.GetSignatureFactory(signature.Ecdsa).NewKeys()

var baseConfig = benchmarkConfig{
	Name: "basic",
	ParallelExecutionConfig: &parallelexecutor.Config{
		BatchSizeCutoff:   100,
		BatchTimeCutoff:   1 * time.Second,
		Parallelism:       4,
		ChannelBufferSize: 1,
	},
	InputGeneratorParams: &sigverification_test.InputGeneratorParams{
		InputDelay: test.NoDelay,
		RequestBatch: sigverification_test.RequestBatchGeneratorParams{
			Tx: sigverification_test.TxGeneratorParams{
				SigningKey:       privateKey,
				Scheme:           signature.Ecdsa,
				ValidSigRatio:    0.8,
				TxSize:           test.Constant(1),
				SerialNumberSize: test.Constant(64),
			},
			BatchSize: test.Constant(100),
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
	for _, parallelism := range []int{1, 3} {
		config.ParallelExecutionConfig.Parallelism = parallelism
		for _, batchSize := range []int64{100} {
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
