/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package coordinator

import (
	"strconv"
	"testing"

	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
)

const (
	// benchSvBatchSize is the batch size the cluster's dependency graph actually produces: the
	// database commits about 340 transactions per batch, and the same batches are what reaches the
	// verifier manager.
	benchSvBatchSize = 340
	// benchSvValueSize approximates a deployed transaction. The cluster's ledger measured 3.34 MB
	// per 10,000-transaction block, so about 334 bytes per transaction. It matters here because the
	// sender goroutine marshals every byte of it inside stream.Send.
	benchSvValueSize = 334
	// benchSvQueueDepth is deliberately far above the deployment's 60, so that the manager's own
	// drain rate is what the benchmark measures rather than the depth of the queue in front of it.
	benchSvQueueDepth = 512
)

// BenchmarkSignatureVerifierManager measures how fast the coordinator's signature verifier manager
// can push transactions through to verifiers and collect their statuses.
//
// The verifiers are mocks that answer without doing any signature work, so whatever ceiling this
// finds belongs to the manager: its per-transaction bookkeeping in txBeingValidated, the proto
// marshalling that stream.Send performs on the sending goroutine, and the fact that there is one
// sender and one receiver goroutine per endpoint.
//
// It exists because the manager had no benchmark, which is why a cluster run could show its input
// queue at 58 of 60 while every other queue in the pipeline read zero and the verifier machines sat
// at 34% CPU.
func BenchmarkSignatureVerifierManager(b *testing.B) {
	for _, numVerifiers := range []int{1, 3} {
		b.Run("verifiers="+strconv.Itoa(numVerifiers), func(b *testing.B) {
			runSvManagerBench(b, numVerifiers)
		})
	}
}

func runSvManagerBench(b *testing.B, numVerifiers int) {
	b.Helper()
	env := newSvMgrTestEnvWithQueues(b, numVerifiers, benchSvQueueDepth)

	// Pre-generate every batch before the timer starts. Generating transactions inside the timed
	// region is what made the first sidecar benchmark unreadable: setup dominated the profile.
	batchCount := max(1, (b.N+benchSvBatchSize-1)/benchSvBatchSize)
	batches := make([]dependencygraph.TxNodeBatch, batchCount)
	for i := range batches {
		batches[i], _ = createTxNodeBatchForTest(b, uint64(i), benchSvBatchSize, benchSvValueSize)
	}
	txTotal := batchCount * benchSvBatchSize

	input := channel.NewWriter(b.Context(), env.inputTxBatch)
	output := channel.NewReader(b.Context(), env.outputValidatedTxs)

	b.ReportAllocs()
	b.ResetTimer()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, batch := range batches {
			if !input.Write(batch) {
				return
			}
		}
	}()

	for received := 0; received < txTotal; {
		validated, ok := output.Read()
		if !ok {
			b.Fatal("context ended before every transaction came back")
		}
		received += len(validated)
	}
	<-done

	b.StopTimer()
	b.ReportMetric(float64(txTotal)/b.Elapsed().Seconds(), "tx/s")
	b.ReportMetric(float64(txTotal)/b.Elapsed().Seconds()/float64(numVerifiers), "tx/s/verifier")
}
