/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package coordinator

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring"
)

const (
	namespace = "coordinator"

	subsystemGRPC      = "grpc"
	subsystemVCService = "vcservice"
)

type (
	perfMetrics struct {
		*monitoring.Provider

		// received and processed transactions
		transactionReceivedTotal  prometheus.Counter
		transactionCommittedTotal *prometheus.CounterVec

		// per-service-manager metrics
		verifiers *managerMetrics
		vcs       *managerMetrics

		// The status queue is read by the coordinator itself, so it has no manager to report it.
		// Its saturation is what stalls the idle handshake, see NoPendingTransactionProcessing.
		vcserviceOutputTxStatusBatchQueueSize prometheus.GaugeFunc

		// transactionInProgress and transactionReady report the two counters the
		// numTxsInProgress >= readyCount >= 0 invariant is written against, so the invariant can
		// be checked from the metrics alone. See NoPendingTransactionProcessing.
		transactionInProgress prometheus.GaugeFunc
		transactionReady      prometheus.GaugeFunc
	}

	// managerMetrics holds the metrics that every service manager reports. Defining them
	// once and instantiating them per manager keeps the two managers from drifting apart.
	managerMetrics struct {
		// connection tracks the connection state to the service endpoints.
		connection *monitoring.ConnectionMetrics
		// processedTotal counts the transactions whose status was received and forwarded.
		processedTotal prometheus.Counter
		// retriedTotal counts the transactions resubmitted after a failure.
		retriedTotal prometheus.Counter
		// inputQueueSize reports the size of the manager's input batch queue.
		inputQueueSize prometheus.GaugeFunc
		// outputQueueSize reports the size of the manager's output batch queue.
		outputQueueSize prometheus.GaugeFunc
		// pendingQueueSize reports the size of the queue the manager's per-endpoint senders draw
		// from, which also receives the batches re-queued after a stream failure.
		pendingQueueSize prometheus.GaugeFunc
	}

	// managerQueues are the queues one service manager reports the size of on scrape. They are
	// only ever measured here and never sent to, so they are receive-only: the direction they are
	// used in elsewhere says nothing about this struct.
	managerQueues struct {
		input   <-chan dependencygraph.TxNodeBatch
		output  <-chan dependencygraph.TxNodeBatch
		pending <-chan dependencygraph.TxNodeBatch
	}
)

func newPerformanceMetrics(q *channels, numTxsInProgress *atomic.Int32) *perfMetrics {
	p := monitoring.NewProvider()

	return &perfMetrics{
		Provider: p,
		transactionReceivedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemGRPC,
			Name:      "received_transaction_total",
			Help:      "Total number of transactions received by the coordinator service from the client.",
		}),
		transactionCommittedTotal: p.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemGRPC,
			Name:      "committed_transaction_total",
			Help:      "Total number of transactions committed status sent by the coordinator service to the client.",
		}, []string{"status"}),
		verifiers: newManagerMetrics(p, monitoring.MetricsParameters{
			Namespace: namespace,
			Subsystem: "verifier",
		}, &managerQueues{
			input:   q.depGraphToSigVerifierFreeTxs,
			output:  q.sigVerifierToVCServiceValidatedTxs,
			pending: q.sigVerifierPendingTxs,
		}),
		vcs: newManagerMetrics(p, monitoring.MetricsParameters{
			Namespace: namespace,
			Subsystem: subsystemVCService,
		}, &managerQueues{
			input:   q.sigVerifierToVCServiceValidatedTxs,
			output:  q.vcServiceToDepGraphValidatedTxs,
			pending: q.vcServicePendingTxs,
		}),
		vcserviceOutputTxStatusBatchQueueSize: monitoring.NewChannelLenGauge(p, prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemVCService,
			Name:      "output_tx_status_batch_queue_size",
			Help: "Size of the output transaction status batch queue of " +
				"the validation and committer service manager.",
		}, q.vcServiceToCoordinatorTxStatus.ch),
		transactionInProgress: monitoring.NewAtomicValueGauge(p, prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemGRPC,
			Name:      "in_progress_transaction",
			Help: "Number of transactions received from the client whose status has not been " +
				"sent back yet.",
		}, numTxsInProgress),
		transactionReady: monitoring.NewAtomicValueGauge(p, prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemGRPC,
			Name:      "ready_transaction",
			Help: "Number of transaction statuses buffered in the output queue, ready to be sent " +
				"to the client.",
		}, &q.vcServiceToCoordinatorTxStatus.count),
	}
}

// newManagerMetrics creates the metric set of a single service manager. The namespace and
// subsystem identify which manager reports them.
func newManagerMetrics(
	p *monitoring.Provider,
	params monitoring.MetricsParameters,
	q *managerQueues,
) *managerMetrics {
	return &managerMetrics{
		connection: monitoring.NewConnectionMetrics(p, params),
		processedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: params.Namespace,
			Subsystem: params.Subsystem,
			Name:      "transaction_processed_total",
			Help:      "Total number of transactions processed by the manager.",
		}),
		retriedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: params.Namespace,
			Subsystem: params.Subsystem,
			Name:      "transaction_retried_total",
			Help:      "Total number of transactions retried by the manager.",
		}),
		inputQueueSize: monitoring.NewChannelLenGauge(p, prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Subsystem: params.Subsystem,
			Name:      "input_batch_queue_size",
			Help:      "Size of the input batch queue of the manager.",
		}, q.input),
		outputQueueSize: monitoring.NewChannelLenGauge(p, prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Subsystem: params.Subsystem,
			Name:      "output_batch_queue_size",
			Help:      "Size of the output batch queue of the manager.",
		}, q.output),
		pendingQueueSize: monitoring.NewChannelLenGauge(p, prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Subsystem: params.Subsystem,
			Name:      "pending_batch_queue_size",
			Help: "Size of the queue the manager's per-endpoint senders draw from, including the " +
				"batches re-queued after a stream failure.",
		}, q.pending),
	}
}
