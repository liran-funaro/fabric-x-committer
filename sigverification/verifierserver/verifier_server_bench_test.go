package verifierserver_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
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
		BatchSizeCutoff:   test.BatchSize,
		BatchTimeCutoff:   sigverification_test.OptimalBatchTimeCutoff,
		Parallelism:       4,
		ChannelBufferSize: sigverification_test.OptimalChannelBufferSize,
	},
	InputGeneratorParams: &sigverification_test.InputGeneratorParams{
		InputDelay: test.NoDelay,
		RequestBatch: sigverification_test.RequestBatchGeneratorParams{
			Tx: sigverification_test.TxGeneratorParams{
				SigningKey:        privateKey,
				Scheme:            sigverification_test.VerificationScheme,
				ValidSigRatio:     sigverification_test.SignatureValidRatio,
				SerialNumberCount: sigverification_test.SerialNumberCountDistribution,
				OutputCount:       sigverification_test.OutputCountDistribution,
				SerialNumberSize:  sigverification_test.SerialNumberSize,
				OutputSize:        sigverification_test.OutputSize,
			},
			BatchSize: sigverification_test.BatchSizeDistribution,
		},
	},
}

func BenchmarkVerifierServer(b *testing.B) {
	output := test.Open("server", &test.ResultOptions{Columns: []*test.ColumnConfig{
		{Header: "Parallelism", Formatter: test.NoFormatting},
		{Header: "Batch size", Formatter: test.ConstantDistributionFormatter},
		{Header: "Throughput", Formatter: test.NoFormatting},
	}})
	defer output.Close()
	var stats connection.RequestTrackerStats
	config := baseConfig
	for _, parallelism := range []int{1, 4, 8, 16, 32, 40, 64, 80} {
		config.ParallelExecutionConfig.Parallelism = parallelism
		for _, batchSize := range []int64{50, 100} {
			config.InputGeneratorParams.RequestBatch.BatchSize = test.Constant(batchSize)
			b.Run(fmt.Sprintf("%s-p%d-b%v", config.Name, config.ParallelExecutionConfig.Parallelism, test.ConstantDistributionFormatter(config.InputGeneratorParams.RequestBatch.BatchSize)), func(b *testing.B) {
				g := sigverification_test.NewInputGenerator(config.InputGeneratorParams)
				m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
				server := verifierserver.New(config.ParallelExecutionConfig, m)
				c := sigverification_test.NewTestState(b, server)
				t := connection.NewRequestTracker()
				defer c.TearDown()
				c.Client.SetVerificationKey(context.Background(), &sigverification.Key{
					NsId:            1,
					SerializedBytes: publicKey,
					Scheme:          config.InputGeneratorParams.RequestBatch.Tx.Scheme,
				})
				stream, _ := c.Client.StartStream(context.Background())
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
				b.StopTimer()
			})
			output.Record(config.ParallelExecutionConfig.Parallelism, config.InputGeneratorParams.RequestBatch.BatchSize, stats.RequestsPer(time.Second))
		}
	}
}
