/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package dependencygraph

import (
	"context"
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

// The invariant a dependency graph manager owes its caller is that every transaction it accepts is
// eventually released, and that it forgets the ones it has seen validated. Nothing else in this
// package tests either over a long run, and the cost of them not holding is total: the graph stops
// draining and the pipeline halts with no error logged anywhere.
//
// Applying load straight to the coordinator on a 19-machine cluster, the simple manager halted
// after committing 45.4 million transactions, holding 1,323,224 that it had accepted and never seen
// validated.
const (
	drainKeySpace   = 32
	drainBatchSize  = 16
	drainBatchCount = 100

	// The saturation test needs channels far smaller than the load it applies, so that every
	// buffer between the graph's output and its validated input is full at once.
	ringChanCap    = 2
	ringBatchSize  = 8
	ringBatchCount = 300
	ringKeysPerTx  = 4
	// A group larger than cap(validatedTxs) + cap(preProcessedValidatedBatchQueue) + the batches
	// the goroutines between them hold.
	ringGroupSize   = 8
	ringGroupCutoff = 5 * time.Millisecond

	// The retention test needs enough transactions that holding all of them is unmistakable.
	forgetTxCount   = 4000
	forgetBatchSize = 8

	drainTimeout    = 30 * time.Second
	drainPollPeriod = 100 * time.Millisecond
)

// TestManagerDrainsEverySubmittedTx submits a long randomised stream over a key space small
// enough to force dependencies, feeds every released transaction straight back as validated, and
// requires that the graph ends up empty. A manager that loses track of a release leaves a
// residue here.
func TestManagerDrainsEverySubmittedTx(t *testing.T) {
	t.Parallel()
	flogging.ActivateSpec("fatal")

	for _, manType := range []string{managerKindGlobalLocal, managerKindSimple} {
		t.Run(manType, func(t *testing.T) {
			t.Parallel()
			requireManagerDrains(t, manType, 0)
		})
	}
}

// TestManagerDrainsWithRejectedTxs adds transactions the coordinator rejects before the graph
// sees them. They reach the validator-committer directly and come back on the validated stream
// (see NewRejectedTransactionNode and the coordinator's rejected path), so a manager that counts
// validated transactions rather than identifying them will drift.
func TestManagerDrainsWithRejectedTxs(t *testing.T) {
	t.Parallel()
	flogging.ActivateSpec("fatal")

	for _, manType := range []string{managerKindGlobalLocal, managerKindSimple} {
		t.Run(manType, func(t *testing.T) {
			t.Parallel()
			requireManagerDrains(t, manType, drainBatchCount/4)
		})
	}
}

func requireManagerDrains(t *testing.T, manType string, rejectedCount int) {
	t.Helper()
	ctx := t.Context()
	keys := makeTestKeys(t, drainKeySpace)

	incomingTxs := make(chan *TransactionBatch, 64)
	outgoingTxs := make(chan TxNodeBatch, 64)
	validatedTxs := make(chan TxNodeBatch, 64)

	metrics := startManager(t, manType, &Parameters{
		IncomingTxs:               incomingTxs,
		OutgoingDepFreeTxsNode:    outgoingTxs,
		IncomingValidatedTxsNode:  validatedTxs,
		NumOfLocalDepConstructors: 2,
		// High enough that the run never reaches it: this is about losing track of a release,
		// not about backpressure.
		WaitingTxsLimit:           drainBatchSize * drainBatchCount * 2,
		QueueMonitorSamplingTime:  DefaultQueueMonitorSamplingTime,
		PrometheusMetricsProvider: monitoring.NewProvider(),
	})

	var released atomic.Int64
	go runValidatingDownstream(ctx, outgoingTxs, validatedTxs, &released)
	if rejectedCount > 0 {
		go runRejectedDownstream(ctx, validatedTxs, rejectedCount)
	}

	rng := rand.New(rand.NewPCG(1, 2))
	submitted := 0
	in := channel.NewWriter(ctx, incomingTxs)
	for batchNum := range drainBatchCount {
		txs := make([]*servicepb.TxWithRef, drainBatchSize)
		for i := range txs {
			// Keys overlap between transactions, so most have to wait for an earlier one, but
			// they are distinct within a transaction: a key repeated across a transaction's own
			// read and write sets makes it wait on itself, which no real workload produces.
			picked := pickDistinctKeys(rng, keys, 5)
			txs[i] = createTxForTest(t, i, nsID1ForTest, picked[0:2], picked[2:4], picked[4:5])
		}
		require.True(t, in.Write(&TransactionBatch{ID: batchID(batchNum), Txs: txs}))
		submitted += len(txs)
	}

	requireEveryTxReleased(t, metrics, &released, submitted)
}

// TestManagerSaturatedPipelineKeepsDraining applies more load than the pipeline around the graph
// can hold, so that every buffer in it is full at the same time.
//
// The coordinator's queues form a ring: the graph's output reaches the verifiers and the
// vcservices, and their results return on the graph's validated input. That ring is bounded end to
// end, and its far side stops consuming the graph's output as soon as its own results have nowhere
// to go, because a gRPC stream whose reader has stopped stops its writer through flow control. A
// manager whose single goroutine both writes the output and drains the validated input therefore
// closes the ring: it blocks on a full output, so it never takes the validated batch that would
// let the output drain. Nothing errors, no goroutine dies, and the pipeline never moves again.
func TestManagerSaturatedPipelineKeepsDraining(t *testing.T) {
	t.Parallel()
	flogging.ActivateSpec("fatal")

	for _, manType := range []string{managerKindGlobalLocal, managerKindSimple} {
		t.Run(manType, func(t *testing.T) {
			t.Parallel()
			requireManagerSurvivesSaturation(t, manType)
		})
	}
}

func requireManagerSurvivesSaturation(t *testing.T, manType string) {
	t.Helper()
	ctx := t.Context()

	incomingTxs := make(chan *TransactionBatch, ringChanCap)
	outgoingTxs := make(chan TxNodeBatch, ringChanCap)
	validatedTxs := make(chan TxNodeBatch, ringChanCap)

	metrics := startManager(t, manType, &Parameters{
		IncomingTxs:               incomingTxs,
		OutgoingDepFreeTxsNode:    outgoingTxs,
		IncomingValidatedTxsNode:  validatedTxs,
		NumOfLocalDepConstructors: 2,
		// The cluster runs with 20,000,000, which is far more than the pipeline can hold, so the
		// limit never engages and cannot be what keeps the graph draining.
		WaitingTxsLimit:           ringBatchSize * ringBatchCount * 2,
		QueueMonitorSamplingTime:  DefaultQueueMonitorSamplingTime,
		PrometheusMetricsProvider: monitoring.NewProvider(),
	})

	var released atomic.Int64
	go runGroupingDownstream(ctx, outgoingTxs, validatedTxs, &released)

	// Every transaction holds keys of its own, so none of them waits for another: this is the
	// shape of the load the cluster was running, whose dependent-transaction gauge was zero
	// throughout. The only key they share is the namespace key that every transaction reads.
	in := channel.NewWriter(ctx, incomingTxs)
	go func() {
		for batchNum := range ringBatchCount {
			txs := make([]*servicepb.TxWithRef, ringBatchSize)
			for i := range txs {
				keys := makeUniqueTestKeys(batchNum*ringBatchSize+i, ringKeysPerTx)
				txs[i] = createTxForTest(t, i, nsID1ForTest, keys[0:1], keys[1:3], keys[3:4])
			}
			if !in.Write(&TransactionBatch{ID: batchID(batchNum), Txs: txs}) {
				return
			}
		}
	}()

	requireEveryTxReleased(t, metrics, &released, ringBatchCount*ringBatchSize)
}

// TestSimpleManagerForgetsValidatedTxs requires that the graph stops referencing a transaction it
// has released and seen validated.
//
// Every transaction reads the key of its namespace, so they all share one key, as readers, for the
// whole life of the load. A manager that keeps the members of a key's running group rather than
// counting them therefore keeps every transaction that ever ran: the group closes only in the
// instant when nothing is in flight, which never comes under sustained load. That is unbounded
// memory, and it grows with the number of transactions committed rather than with the number
// waiting -- which is why it takes tens of millions of transactions to show up as a problem.
func TestSimpleManagerForgetsValidatedTxs(t *testing.T) {
	t.Parallel()
	flogging.ActivateSpec("fatal")
	ctx := t.Context()

	incomingTxs := make(chan *TransactionBatch, 8)
	outgoingTxs := make(chan TxNodeBatch, 8)
	validatedTxs := make(chan TxNodeBatch, 8)

	metrics := startManager(t, managerKindSimple, &Parameters{
		IncomingTxs:               incomingTxs,
		OutgoingDepFreeTxsNode:    outgoingTxs,
		IncomingValidatedTxsNode:  validatedTxs,
		NumOfLocalDepConstructors: 1,
		WaitingTxsLimit:           forgetTxCount * 2,
		QueueMonitorSamplingTime:  DefaultQueueMonitorSamplingTime,
		PrometheusMetricsProvider: monitoring.NewProvider(),
	})

	var released atomic.Int64
	go runHoldingFirstBatchDownstream(ctx, outgoingTxs, validatedTxs, &released)

	// A cleanup fires once nothing references the transaction any more. It must not close over the
	// transaction it is attached to, or it would keep it alive forever.
	var collected atomic.Int64
	in := channel.NewWriter(ctx, incomingTxs)
	submitted := 0
	for batchNum := range forgetTxCount / forgetBatchSize {
		txs := make([]*servicepb.TxWithRef, forgetBatchSize)
		for i := range txs {
			keys := makeUniqueTestKeys(batchNum*forgetBatchSize+i, ringKeysPerTx)
			txs[i] = createTxForTest(t, i, nsID1ForTest, keys[0:1], keys[1:3], keys[3:4])
			runtime.AddCleanup(txs[i], func(int) { collected.Add(1) }, 0)
		}
		require.True(t, in.Write(&TransactionBatch{ID: batchID(batchNum), Txs: txs}))
		submitted += len(txs)
	}

	// Everything but the batch the stand-in holds back must be released and validated.
	expectedReleased := int64(submitted - forgetBatchSize)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.Equal(ct, expectedReleased, released.Load())
		require.Equal(ct, forgetBatchSize, test.GetIntMetricValue(ct, metrics.gdgWaitingTxQueueSize))
	}, drainTimeout, drainPollPeriod)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		runtime.GC()
		// A handful may still be reachable from a stack or from a queue that has not been reused.
		require.Greater(ct, collected.Load(), expectedReleased*3/4)
	}, drainTimeout, drainPollPeriod)
}

// runValidatingDownstream stands in for the pipeline that follows the graph: whatever the graph
// frees is immediately validated. The count is an atomic rather than a channel so that this never
// blocks on the assertions -- a stand-in that stops reading the output deadlocks the manager it is
// testing.
func runValidatingDownstream(
	ctx context.Context, outgoingTxs <-chan TxNodeBatch, validatedTxs chan<- TxNodeBatch, released *atomic.Int64,
) {
	out := channel.NewReader(ctx, outgoingTxs)
	back := channel.NewWriter(ctx, validatedTxs)
	for {
		batch, ok := out.Read()
		if !ok {
			return
		}
		released.Add(int64(len(batch)))
		if !back.Write(batch) {
			return
		}
	}
}

// runGroupingDownstream stands in for the verifiers and the vcservices as one: they take batches
// from the graph, work on a group of them together, and return the group's results in a lump,
// cutting the group on size or on time as a vcservice does (see MinTransactionBatchSize and
// TimeoutForMinTransactionBatchSize). While returning a lump they take nothing new, which is what a
// gRPC stream does to its writer once its reader has stopped. A group is larger than every queue
// between the graph's output and its validated input, so a lump cannot be returned unless the graph
// keeps accepting validated batches while its own output waits.
func runGroupingDownstream(
	ctx context.Context, outgoingTxs <-chan TxNodeBatch, validatedTxs chan<- TxNodeBatch, released *atomic.Int64,
) {
	back := channel.NewWriter(ctx, validatedTxs)
	for ctx.Err() == nil {
		group := make([]TxNodeBatch, 0, ringGroupSize)
		cutoff := time.After(ringGroupCutoff)
		for len(group) < ringGroupSize {
			select {
			case batch := <-outgoingTxs:
				group = append(group, batch)
				continue
			case <-cutoff:
			case <-ctx.Done():
			}
			break
		}
		for _, batch := range group {
			released.Add(int64(len(batch)))
			if !back.Write(batch) {
				return
			}
		}
	}
}

// runHoldingFirstBatchDownstream never validates the first batch it is given, so the namespace key
// that every transaction reads keeps a running group for the whole test, as it does in production.
func runHoldingFirstBatchDownstream(
	ctx context.Context, outgoingTxs <-chan TxNodeBatch, validatedTxs chan<- TxNodeBatch, released *atomic.Int64,
) {
	out := channel.NewReader(ctx, outgoingTxs)
	back := channel.NewWriter(ctx, validatedTxs)
	var held []TxNodeBatch
	for {
		batch, ok := out.Read()
		if !ok {
			return
		}
		if len(held) == 0 {
			held = append(held, batch)
			continue
		}
		released.Add(int64(len(batch)))
		if !back.Write(batch) {
			return
		}
	}
}

// runRejectedDownstream returns transactions the coordinator rejected before the graph saw them.
// Such a transaction never entered the graph, so it carries no keys and no waiters.
func runRejectedDownstream(ctx context.Context, validatedTxs chan<- TxNodeBatch, count int) {
	back := channel.NewWriter(ctx, validatedTxs)
	for range count {
		if !back.Write(TxNodeBatch{NewRejectedTransactionNode(&committerpb.TxStatus{
			Ref:    committerpb.NewTxRef(uuid.New().String(), 0, 0),
			Status: committerpb.Status_ABORTED_SIGNATURE_INVALID,
		})}) {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// requireEveryTxReleased waits for the graph to release everything it was given. The graph's own
// gauge is the clearest statement of the invariant -- nothing left waiting -- but on its own it
// also holds before the first transaction arrives, so the released count is checked with it.
func requireEveryTxReleased(t *testing.T, metrics *perfMetrics, released *atomic.Int64, submitted int) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.Equal(ct, int64(submitted), released.Load(),
			"every submitted transaction must be released exactly once")
		require.Equal(ct, 0, test.GetIntMetricValue(ct, metrics.gdgWaitingTxQueueSize),
			"no transaction may be left waiting")
	}, drainTimeout, drainPollPeriod)
}

// pickDistinctKeys samples n keys without replacement, so no key appears twice in one
// transaction.
func pickDistinctKeys(rng *rand.Rand, keys [][]byte, n int) [][]byte {
	chosen := make(map[int]struct{}, n)
	picked := make([][]byte, 0, n)
	for len(picked) < n {
		idx := rng.IntN(len(keys))
		if _, seen := chosen[idx]; seen {
			continue
		}
		chosen[idx] = struct{}{}
		picked = append(picked, keys[idx])
	}
	return picked
}

// makeUniqueTestKeys returns n keys used by no other transaction.
func makeUniqueTestKeys(txNum, n int) [][]byte {
	keys := make([][]byte, n)
	for i := range keys {
		keys[i] = fmt.Appendf(nil, "%d-%d", txNum, i)
	}
	return keys
}

// batchID numbers a batch as the coordinator's txBatchIDToDepGraph does. The local dependency
// constructor orders its output by this ID and expects the first one to be 1: a batch numbered 0
// waits forever for a predecessor that cannot exist.
func batchID(batchNum int) uint64 {
	return uint64(batchNum) + 1 //nolint:gosec // int -> uint64.
}
