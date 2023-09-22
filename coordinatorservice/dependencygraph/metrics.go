package dependencygraph

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
)

type perfMetrics struct {
	enabled  bool
	provider *prometheusmetrics.Provider

	// queue sizes
	localDependencyGraphInputTxBatchQueueSize  prometheus.Gauge
	globalDependencyGraphInputTxBatchQueueSize prometheus.Gauge
	globalDependencyGraphWaitingTxQueueSize    prometheus.Gauge

	// processed transactions by each manager
	localDependencyGraphTransactionProcessedTotal           prometheus.Counter
	globalDependencyGraphTransactionProcessedTotal          prometheus.Counter
	globalDependencyGraphValidatedTransactionProcessedTotal prometheus.Counter
}

func newPerformanceMetrics(enabled bool, p *prometheusmetrics.Provider) *perfMetrics {
	return &perfMetrics{
		enabled:  enabled,
		provider: p,
		localDependencyGraphInputTxBatchQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "local_dependency_graph",
			Name:      "input_tx_batch_queue_size",
			Help:      "Size of the input transaction batch queue of the local dependency graph manager",
		}),
		globalDependencyGraphInputTxBatchQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "global_dependency_graph",
			Name:      "input_tx_batch_queue_size",
			Help:      "Size of the input transaction batch queue of the global dependency graph manager",
		}),
		globalDependencyGraphWaitingTxQueueSize: p.NewGauge(prometheus.GaugeOpts{
			Namespace: "coordinator",
			Subsystem: "global_dependency_graph",
			Name:      "waiting_tx_queue_size",
			Help:      "Size of the waiting transaction queue of the global dependency graph manager",
		}),
		localDependencyGraphTransactionProcessedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "coordinator",
			Subsystem: "local_dependency_graph",
			Name:      "transaction_processed_total",
			Help:      "Total number of transactions processed by the local dependency graph manager",
		}),
		globalDependencyGraphTransactionProcessedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "coordinator",
			Subsystem: "global_dependency_graph",
			Name:      "transaction_processed_total",
			Help:      "Total number of transactions processed by the global dependency graph manager",
		}),
		globalDependencyGraphValidatedTransactionProcessedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "coordinator",
			Subsystem: "global_dependency_graph",
			Name:      "validated_transaction_processed_total",
			Help:      "Total number of validated transactions processed by the global dependency graph manager",
		}),
	}
}

func (s *perfMetrics) addToCounter(c prometheus.Counter, n int) {
	if s.enabled {
		c.Add(float64(n))
	}
}

func (s *perfMetrics) setQueueSize(queue prometheus.Gauge, size int) {
	if s.enabled {
		queue.Set(float64(size))
	}
}
