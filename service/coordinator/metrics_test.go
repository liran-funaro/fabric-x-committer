/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package coordinator

import (
	"math"
	"sync/atomic"
	"testing"

	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring/promutil"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

// TestManagerMetricsSubsystem asserts on the exported series names rather than on the
// perfMetrics fields. The two managers previously reported each other's retries because the
// counters were registered under each other's subsystem, and the tests that covered them
// asserted on the Go field, so they passed while the exported series was wrong.
func TestManagerMetricsSubsystem(t *testing.T) {
	t.Parallel()
	m := newTestPerfMetrics()

	promutil.AddToCounter(m.verifiers.processedTotal, 3)
	promutil.AddToCounter(m.verifiers.retriedTotal, 5)
	promutil.AddToCounter(m.vcs.processedTotal, 7)
	promutil.AddToCounter(m.vcs.retriedTotal, 11)

	for name, expected := range map[string]int{
		"coordinator_verifier_transaction_processed_total":  3,
		"coordinator_verifier_transaction_retried_total":    5,
		"coordinator_vcservice_transaction_processed_total": 7,
		"coordinator_vcservice_transaction_retried_total":   11,
	} {
		require.Equal(t, expected, gatherCounter(t, m, name), "series %s", name)
	}
}

// TestManagerQueueMetricsAreWired fills each manager queue and asserts the gauge reporting it
// follows. Both managers export the same series names under different subsystems, so a gauge
// attached to the wrong queue would otherwise go unnoticed.
func TestManagerQueueMetricsAreWired(t *testing.T) {
	t.Parallel()

	q := &channels{
		depGraphToSigVerifierFreeTxs:       make(chan dependencygraph.TxNodeBatch, 4),
		sigVerifierToVCServiceValidatedTxs: make(chan dependencygraph.TxNodeBatch, 4),
		vcServiceToDepGraphValidatedTxs:    make(chan dependencygraph.TxNodeBatch, 4),
		vcServiceToCoordinatorTxStatus:     newTxStatusQueue(4),
		sigVerifierPendingTxs:              make(chan dependencygraph.TxNodeBatch, 4),
		vcServicePendingTxs:                make(chan dependencygraph.TxNodeBatch, 4),
	}
	m := newPerformanceMetrics(q, &atomic.Int32{})

	q.depGraphToSigVerifierFreeTxs <- dependencygraph.TxNodeBatch{}
	q.sigVerifierToVCServiceValidatedTxs <- dependencygraph.TxNodeBatch{}
	q.sigVerifierToVCServiceValidatedTxs <- dependencygraph.TxNodeBatch{}
	q.sigVerifierPendingTxs <- dependencygraph.TxNodeBatch{}

	test.RequireIntMetricValue(t, 1, m.verifiers.inputQueueSize)
	test.RequireIntMetricValue(t, 2, m.verifiers.outputQueueSize)
	// The verifier's output is the vcservice's input, so both report the same queue.
	test.RequireIntMetricValue(t, 2, m.vcs.inputQueueSize)
	test.RequireIntMetricValue(t, 0, m.vcs.outputQueueSize)

	// Each manager reports its own pending queue, not the other's.
	test.RequireIntMetricValue(t, 1, m.verifiers.pendingQueueSize)
	test.RequireIntMetricValue(t, 0, m.vcs.pendingQueueSize)
}

// TestInvariantCountersAreReported covers the two gauges over the counters the
// numTxsInProgress >= readyCount >= 0 invariant is written against. Both are reported so the
// invariant can be checked from the metrics alone, so each must follow its own counter.
func TestInvariantCountersAreReported(t *testing.T) {
	t.Parallel()

	numTxsInProgress := &atomic.Int32{}
	statusQueue := newTxStatusQueue(4)
	m := newPerformanceMetrics(&channels{
		vcServiceToCoordinatorTxStatus: statusQueue,
	}, numTxsInProgress)

	test.RequireIntMetricValue(t, 0, m.transactionInProgress)
	test.RequireIntMetricValue(t, 0, m.transactionReady)

	// A block arrives: three transactions are in progress, none has a status yet.
	numTxsInProgress.Add(3)
	test.RequireIntMetricValue(t, 3, m.transactionInProgress)
	test.RequireIntMetricValue(t, 0, m.transactionReady)

	// Two statuses come back from the VC, so they are queued and ready to send.
	require.True(t, statusQueue.write(t.Context(), &committerpb.TxStatusBatch{
		Status: []*committerpb.TxStatus{{}, {}},
	}))
	test.RequireIntMetricValue(t, 3, m.transactionInProgress)
	test.RequireIntMetricValue(t, 2, m.transactionReady)

	// Sending them to the client drains the queue and decrements both.
	_, ok := statusQueue.read(t.Context())
	require.True(t, ok)
	numTxsInProgress.Add(-2)
	test.RequireIntMetricValue(t, 1, m.transactionInProgress)
	test.RequireIntMetricValue(t, 0, m.transactionReady)
}

// newTestPerfMetrics builds the metric set over a minimal set of queues. The status queue must be
// constructed: newPerformanceMetrics reads its channel to register the gauge, so a nil queue
// panics there, whereas a nil channel is simply a queue whose length is always zero.
func newTestPerfMetrics() *perfMetrics {
	return newPerformanceMetrics(&channels{
		vcServiceToCoordinatorTxStatus: newTxStatusQueue(1),
	}, &atomic.Int32{})
}

// gatherCounter returns the value of the named counter series as the registry exports it,
// rounded to the nearest integer as the counters here all count transactions.
func gatherCounter(t *testing.T, m *perfMetrics, name string) int {
	t.Helper()
	families, err := m.Registry().Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		require.Len(t, family.GetMetric(), 1)
		return int(math.Round(family.GetMetric()[0].GetCounter().GetValue()))
	}
	require.Failf(t, "metric not found", "no series named %s is registered", name)
	return 0
}
