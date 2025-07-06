/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package dependencygraph

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/api/protoblocktx"
	"github.com/hyperledger/fabric-x-committer/api/types"
	"github.com/hyperledger/fabric-x-committer/loadgen/workload"
	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/logging"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

type batchWithLatency struct {
	batch TxNodeBatch
	done  time.Time
}

//nolint:gocognit // single method for simplicity.
func BenchmarkDependencyGraph(b *testing.B) {
	logging.SetupWithConfig(&logging.Config{Enabled: false})

	// Parameters
	latency := 10 * time.Second
	batchSize := 1024
	nums := utils.Range(0, uint32(batchSize)) //nolint:gosec // int -> uint32.

	testKeysCount := []int{2, 4, 6}
	allDep := []string{workload.DependencyReadOnly, workload.DependencyReadWrite, workload.DependencyBlindWrite}

	// We start with one to have no dependencies.
	testDependencies := make([]workload.DependencyDescription, 1, 1+len(allDep)*len(allDep))
	for _, src := range allDep {
		for _, dst := range allDep {
			testDependencies = append(testDependencies, workload.DependencyDescription{
				Probability: 0.3,
				Gap:         workload.NewNormalDistribution(500, 10),
				Src:         src,
				Dst:         dst,
			})
		}
	}

	for _, keyCount := range testKeysCount {
		b.Run(fmt.Sprintf("%d-keys", keyCount), func(b *testing.B) {
			for idx, dep := range testDependencies {
				p := workload.DefaultProfile(8)
				name := "no-dep"
				if idx > 0 {
					p.Conflicts.Dependencies = []workload.DependencyDescription{dep}
					name = fmt.Sprintf("%s AND %s", dep.Src, dep.Dst)
				}
				p.Transaction.ReadWriteCount = workload.NewConstantDistribution(float64(keyCount))
				g := workload.StartGenerator(b, p)

				b.Run(name, func(b *testing.B) {
					for _, tc := range []struct {
						kind    string
						workers int
					}{
						{kind: "simple", workers: 2},
						{kind: "global-local", workers: 2},
						{kind: "global-local", workers: 4},
					} {
						name := fmt.Sprintf("%s-%d", tc.kind, tc.workers)
						b.Run(name, func(b *testing.B) {
							// The main queues.
							in := make(chan *TransactionBatch, 8)
							out := make(chan TxNodeBatch, 8)
							val := make(chan TxNodeBatch, 8)

							// Starts the dependency manager.
							startManager(b, tc.kind, &Parameters{
								IncomingTxs:               in,
								OutgoingDepFreeTxsNode:    out,
								IncomingValidatedTxsNode:  val,
								NumOfLocalDepConstructors: tc.workers,
								WaitingTxsLimit:           20_000_000,
								PrometheusMetricsProvider: monitoring.NewProvider(),
							})

							ctx := b.Context()
							outCtx := channel.NewReader(ctx, out)
							valCtx := channel.NewWriter(ctx, val)
							inCtx := channel.NewWriter(ctx, in)
							latencySimulatorQueue := channel.Make[*batchWithLatency](ctx, 1024*1024)

							// Simulates the batch processing latency.
							go func() {
								for ctx.Err() == nil {
									wtl, ok := latencySimulatorQueue.Read()
									if !ok {
										return
									}
									waitFor := time.Until(wtl.done)
									if waitFor > 0 {
										select {
										case <-time.After(waitFor):
										case <-ctx.Done():
										}
									}
									valCtx.Write(wtl.batch)
								}
							}()

							b.ResetTimer()
							// Generates the load to the manager's queue.
							go func() {
								var i uint64
								for ctx.Err() == nil {
									txs := g.NextN(ctx, batchSize)
									inCtx.Write(&TransactionBatch{
										ID:          i,
										BlockNumber: i,
										Txs:         txs,
										TxsNum:      nums,
									})
									i++
								}
							}()
							// Reads the output of the manager, and forward it to the latency simulator.
							var total int
							for total < b.N {
								batch, ok := outCtx.Read()
								if !ok {
									return
								}
								latencySimulatorQueue.Write(&batchWithLatency{
									batch: batch,
									done:  time.Now().Add(latency),
								})
								total += len(batch)
							}
							b.StopTimer()
						})
					}
				})
			}
		})
	}
}

func TestDependencyGraphManager(t *testing.T) {
	t.Parallel()

	keysPoll := makeTestKeys(t, 10)
	keys := func(idx ...int) [][]byte {
		k := make([][]byte, len(idx))
		for i := range idx {
			k[i] = keysPoll[idx[i]]
		}
		return k
	}

	for _, manType := range []string{"global-local", "simple"} {
		t.Run(manType, func(t *testing.T) {
			t.Parallel()
			incomingTxs := make(chan *TransactionBatch, 10)
			outgoingTxs := make(chan TxNodeBatch, 10)
			validatedTxs := make(chan TxNodeBatch, 10)

			manService, metrics := startManager(t, manType, &Parameters{
				IncomingTxs:               incomingTxs,
				OutgoingDepFreeTxsNode:    outgoingTxs,
				IncomingValidatedTxsNode:  validatedTxs,
				NumOfLocalDepConstructors: 2,
				WaitingTxsLimit:           20,
				PrometheusMetricsProvider: monitoring.NewProvider(),
			})

			t.Log("check reads and writes dependency tracking")
			test.RequireIntMetricValue(t, 0, metrics.dependentTransactionsQueueSize)

			// t2 depends on t1
			t1 := createTxForTest(t, nsID1ForTest, keys(0, 1), keys(2, 3), keys(4, 5))
			t2 := createTxForTest(t, nsID1ForTest, keys(4, 5), keys(2, 6), keys(3, 7))

			incomingTxs <- &TransactionBatch{
				ID:     1,
				Txs:    []*protoblocktx.Tx{t1, t2},
				TxsNum: []uint32{0, 1},
			}

			test.EventuallyIntMetric(t, 1, metrics.dependentTransactionsQueueSize, 5*time.Second, 100*time.Millisecond)

			// t3 depends on t2 and t1
			t3 := createTxForTest(t, nsID1ForTest, keys(7), keys(2, 3), keys(8, 5))
			// t4 depends on t2 and t1
			t4 := createTxForTest(t, nsID1ForTest, keys(7, 6), keys(4, 1), keys(0, 9))

			incomingTxs <- &TransactionBatch{
				ID:     2,
				Txs:    []*protoblocktx.Tx{t3, t4},
				TxsNum: []uint32{0, 1},
			}

			test.EventuallyIntMetric(t, 3, metrics.dependentTransactionsQueueSize, 5*time.Second, 100*time.Millisecond)

			// only t1 is dependency free
			depFreeTxs := <-outgoingTxs
			require.Len(t, depFreeTxs, 1)
			actualT1 := depFreeTxs[0]
			require.Equal(t, t1.Id, actualT1.Tx.ID)
			ensureNoOutputs(t, outgoingTxs)

			validatedTxs <- TxNodeBatch{actualT1}

			// after t1 is validated, t2 is dependency free
			depFreeTxs = <-outgoingTxs
			require.Len(t, depFreeTxs, 1)
			actualT2 := depFreeTxs[0]
			require.Equal(t, t2.Id, actualT2.Tx.ID)
			ensureNoOutputs(t, outgoingTxs)

			test.RequireIntMetricValue(t, 2, metrics.dependentTransactionsQueueSize)

			validatedTxs <- TxNodeBatch{actualT2}

			// after t2 is validated, both t3 and t4 are dependency free
			depFreeTxs = <-outgoingTxs
			require.Len(t, depFreeTxs, 2)
			var actualT3, actualT4 *TransactionNode
			if t3.Id == depFreeTxs[0].Tx.ID {
				actualT3 = depFreeTxs[0]
				actualT4 = depFreeTxs[1]
			} else {
				actualT3 = depFreeTxs[1]
				actualT4 = depFreeTxs[0]
			}
			require.Equal(t, t3.Id, actualT3.Tx.ID)
			require.Equal(t, t4.Id, actualT4.Tx.ID)
			ensureNoOutputs(t, outgoingTxs)

			test.RequireIntMetricValue(t, 0, metrics.dependentTransactionsQueueSize)

			validatedTxs <- TxNodeBatch{actualT3, actualT4}

			ensureProcessedAndValidatedMetrics(t, metrics, 4, 4)
			// after validating all txs, the dependency detector should be empty
			ensureEmptyManager(t, manService)

			t.Log("check dependency in namespace")
			// t2 depends on t1, t1 depends on t0.
			t0 := createTxForTest(
				t, types.ConfigNamespaceID, nil, nil, [][]byte{[]byte(types.ConfigKey)},
			)
			t1 = createTxForTest(
				t, types.MetaNamespaceID, nil, [][]byte{[]byte(nsID1ForTest)}, nil,
			)
			t2 = createTxForTest(
				t, nsID1ForTest, keys(4, 5), keys(2, 6), keys(3, 7),
			)

			incomingTxs <- &TransactionBatch{
				ID:     3,
				Txs:    []*protoblocktx.Tx{t0, t1, t2},
				TxsNum: []uint32{0, 1, 2},
			}

			// t3 depends on t2, t1, and t0
			t3 = createTxForTest(
				t, types.MetaNamespaceID, nil, [][]byte{[]byte(nsID1ForTest)}, nil,
			)
			// t4 depends on t3, t2 and t1
			t4 = createTxForTest(t, nsID1ForTest, keys(7, 6), keys(4, 1), keys(0, 9))

			incomingTxs <- &TransactionBatch{
				ID:     4,
				Txs:    []*protoblocktx.Tx{t3, t4},
				TxsNum: []uint32{0, 1},
			}

			// only t0 is dependency free
			depFreeTxs = <-outgoingTxs
			require.Len(t, depFreeTxs, 1)
			actualT0 := depFreeTxs[0]
			require.Equal(t, t0.Id, actualT0.Tx.ID)
			ensureNoOutputs(t, outgoingTxs)

			validatedTxs <- TxNodeBatch{actualT0}

			// only t1 is dependency free
			depFreeTxs = <-outgoingTxs
			require.Len(t, depFreeTxs, 1)
			actualT1 = depFreeTxs[0]
			require.Equal(t, t1.Id, actualT1.Tx.ID)
			ensureNoOutputs(t, outgoingTxs)

			validatedTxs <- TxNodeBatch{actualT1}

			// after t1 is validated, t2 is dependency free
			depFreeTxs = <-outgoingTxs
			require.Len(t, depFreeTxs, 1)
			actualT2 = depFreeTxs[0]
			require.Equal(t, t2.Id, actualT2.Tx.ID)
			ensureNoOutputs(t, outgoingTxs)

			validatedTxs <- TxNodeBatch{actualT2}

			// after t2 is validated, t3 becomes dependency free
			depFreeTxs = <-outgoingTxs
			require.Len(t, depFreeTxs, 1)
			actualT3 = depFreeTxs[0]
			require.Equal(t, t3.Id, actualT3.Tx.ID)
			ensureNoOutputs(t, outgoingTxs)

			validatedTxs <- TxNodeBatch{actualT3}

			// after t3 is validated, t4 becomes dependency free
			depFreeTxs = <-outgoingTxs
			require.Len(t, depFreeTxs, 1)
			actualT4 = depFreeTxs[0]
			require.Equal(t, t4.Id, actualT4.Tx.ID)
			ensureNoOutputs(t, outgoingTxs)

			validatedTxs <- TxNodeBatch{actualT4}

			ensureProcessedAndValidatedMetrics(t, metrics, 9, 9)
			// after validating all txs, the dependency detector should be empty
			ensureEmptyManager(t, manService)
		})
	}
}

func ensureEmptyManager(t *testing.T, m any) {
	t.Helper()
	require.Eventually(t, func() bool {
		switch mt := m.(type) {
		case *Manager:
			d := mt.globalDepManager.dependencyDetector
			w := len(d.readOnlyKeyToWaitingTxs) + len(d.writeOnlyKeyToWaitingTxs) + len(d.readWriteKeyToWaitingTxs)
			return w == 0
		case *SimpleManager:
			return mt.waitingTXs == 0
		default:
			return false
		}
	}, 2*time.Second, 100*time.Millisecond)
}

func ensureNoOutputs(t *testing.T, outgoingTxs <-chan TxNodeBatch) {
	t.Helper()
	select {
	case badDepTXs := <-outgoingTxs:
		t.Fatalf("got transactions: %v", badDepTXs)
	case <-time.After(time.Second):
		// Fantastic.
	}
}

func startManager(tb testing.TB, kind string, p *Parameters) (any, *perfMetrics) {
	tb.Helper()
	var manService interface {
		Run(context.Context)
	}
	var metrics *perfMetrics
	switch kind {
	case "global-local":
		m := NewManager(p)
		manService = m
		metrics = m.metrics
	case "simple":
		m := NewSimpleManager(p)
		manService = m
		metrics = m.metrics
	default:
		return nil, nil
	}
	test.RunServiceForTest(tb.Context(), tb, func(ctx context.Context) error {
		manService.Run(ctx)
		return nil
	}, nil)
	return manService, metrics
}
