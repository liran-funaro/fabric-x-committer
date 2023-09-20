package coordinatorservice

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
)

type perfMetrics struct {
	enabled                                    bool
	provider                                   *prometheusmetrics.Provider
	transactionReceivedTotal                   prometheus.Counter
	transactionCommittedStatusSentTotal        prometheus.Counter
	transactionMVCCConflictStatusSentTotal     prometheus.Counter
	transactionInvalidSignatureStatusSentTotal prometheus.Counter
	transactionDuplicateTxStatusSentTotal      prometheus.Counter
	sigverifierInputBlockQueueSize             prometheus.Gauge
	sigverifierOutputValidBlockQueueSize       prometheus.Gauge
	sigverifierOutputInvalidBlockQueueSize     prometheus.Gauge
	dependencyGraphInputTxBatchQueueSize       prometheus.Gauge
	vcserviceInputTxBatchQueueSize             prometheus.Gauge
	vcserviceOutputTxStatusBatchQueueSize      prometheus.Gauge
	vcserviceOutputValidatedTxBatchQueueSize   prometheus.Gauge
}

func newCoordinatorServiceMetrics(enabled bool) *perfMetrics {
	p := prometheusmetrics.NewProvider()

	return &perfMetrics{
		enabled:  enabled,
		provider: p,
		transactionReceivedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "coordinator",
			Subsystem: "grpc",
			Name:      "received_transaction_total",
			Help:      "Number of transactions received by the coordinator service from the client",
		}),
		transactionCommittedStatusSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "coordinator",
			Subsystem: "grpc",
			Name:      "sent_transaction_committed_status_total",
			Help:      "Number of transactions committed status sent by the coordinator service to the client",
		}),
		transactionMVCCConflictStatusSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "coordinator",
			Subsystem: "grpc",
			Name:      "sent_transaction_mvcc_conflict_status_total",
			Help:      "Number of transactions mvcc conflict status sent by the coordinator service to the client",
		}),
		transactionInvalidSignatureStatusSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "coordinator",
			Subsystem: "grpc",
			Name:      "sent_transaction_invalid_signature_status_total",
			Help:      "Number of transactions invalid signature status sent by the coordinator service to the client",
		}),
		transactionDuplicateTxStatusSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "coordinator",
			Subsystem: "grpc",
			Name:      "sent_transaction_duplicate_tx_status_total",
			Help:      "Number of transactions duplicate tx status sent by the coordinator service to the client",
		}),
		sigverifierInputBlockQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "sigverifier",
			Name:      "input_block_queue_size",
			Help:      "Size of the input block queue of the signature verifier manager",
		}),
		sigverifierOutputValidBlockQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "sigverifier",
			Name:      "output_valid_block_queue_size",
			Help:      "Size of the output valid block queue of the signature verifier manager",
		}),
		sigverifierOutputInvalidBlockQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "sigverifier",
			Name:      "output_invalid_block_queue_size",
			Help:      "Size of the output invalid block queue of the signature verifier manager",
		}),
		dependencyGraphInputTxBatchQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "dependencygraph",
			Name:      "input_tx_batch_queue_size",
			Help:      "Size of the input transaction batch queue of the dependency graph manager",
		}),
		vcserviceInputTxBatchQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "vcservice",
			Name:      "input_tx_batch_queue_size",
			Help:      "Size of the input transaction batch queue of the validation and consensus service manager",
		}),
		vcserviceOutputTxStatusBatchQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "vcservice",
			Name:      "output_tx_status_batch_queue_size",
			Help: "Size of the output transaction status batch queue of " +
				"the validation and consensus service manager",
		}),
		vcserviceOutputValidatedTxBatchQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "vcservice",
			Name:      "output_validated_tx_batch_queue_size",
			Help: "Size of the output validated transaction batch queue " +
				"of the validation and consensus service manager",
		}),
	}
}

func (s *perfMetrics) transactionReceived(n int) {
	if s.enabled {
		s.transactionReceivedTotal.Add(float64(n))
	}
}

func (s *perfMetrics) transactionStatusSent(c prometheus.Counter, n int) {
	if s.enabled {
		c.Add(float64(n))
	}
}

func (s *perfMetrics) setQueueSize(queue prometheus.Gauge, size int) {
	if s.enabled {
		queue.Set(float64(size))
	}
}
