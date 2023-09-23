package dependencygraph

import (
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/workerpool"
)

type (
	// Manager is the main component of the dependency graph module.
	// It is responsible for managing the local dependency constructor
	// and the global dependency manager.
	Manager struct {
		localDepConstructor         *localDependencyConstructor
		globalDepManager            *globalDependencyManager
		config                      *Config
		outgoingTxsNodeWithLocalDep chan *transactionNodeBatch
		metrics                     *perfMetrics
	}

	// Config holds the configuration for the dependency graph manager.
	Config struct {
		// IncomingTxs is the channel for dependency manager to receive
		// incoming transactions.
		IncomingTxs <-chan *TransactionBatch
		// OutgoingDepFreeTxsNode is the channel dependency manager to send
		// dependency free transactions for validation and commit.
		OutgoingDepFreeTxsNode chan<- []*TransactionNode
		// IncomingValidatedTxsNode is the channel for dependency manager
		// to receive validated transactions.
		IncomingValidatedTxsNode <-chan []*TransactionNode
		// NumOfLocalDepConstructors defines the number of local
		// dependency constructors.
		NumOfLocalDepConstructors int
		// WorkerPoolConfigForGlobalDepManager defines the worker pool
		// for the global dependency manager.
		WorkerPoolConfigForGlobalDepManager *workerpool.Config
		// WaitingTxsLimit defines the maximum number of transactions
		// that can be waiting at the dependency manager.
		WaitingTxsLimit int
		// PrometheusMetricsProvider is the provider for Prometheus metrics.
		PrometheusMetricsProvider *prometheusmetrics.Provider
		// MetricsEnabled defines whether metrics are enabled.
		MetricsEnabled bool
	}
)

// NewManager creates a new dependency graph manager.
func NewManager(c *Config) *Manager {
	metrics := newPerformanceMetrics(c.MetricsEnabled, c.PrometheusMetricsProvider)

	outgoingTxsNodeWithLocalDep := make(chan *transactionNodeBatch, cap(c.IncomingTxs))
	ldp := newLocalDependencyConstructor(c.IncomingTxs, outgoingTxsNodeWithLocalDep, metrics)

	gdConf := &globalDepConfig{
		incomingTxsNode:        outgoingTxsNodeWithLocalDep,
		outgoingDepFreeTxsNode: c.OutgoingDepFreeTxsNode,
		validatedTxsNode:       c.IncomingValidatedTxsNode,
		workerPoolConfig:       c.WorkerPoolConfigForGlobalDepManager,
		waitingTxsLimit:        c.WaitingTxsLimit,
		metrics:                metrics,
	}

	gdp := newGlobalDependencyManager(gdConf)

	return &Manager{
		localDepConstructor:         ldp,
		globalDepManager:            gdp,
		config:                      c,
		outgoingTxsNodeWithLocalDep: outgoingTxsNodeWithLocalDep,
		metrics:                     metrics,
	}
}

// Start starts the dependency graph manager by starting the
// local dependency constructors and global dependency graph manager.
func (m *Manager) Start() {
	go m.monitorQueues()
	m.localDepConstructor.start(m.config.NumOfLocalDepConstructors)
	m.globalDepManager.start()
}

func (m *Manager) monitorQueues() {
	// TODO: make sampling time configurable
	ticker := time.NewTicker(500 * time.Millisecond)
	for {
		<-ticker.C

		m.metrics.setQueueSize(
			m.metrics.localDependencyGraphInputTxBatchQueueSize,
			len(m.localDepConstructor.incomingTransactions),
		)
		m.metrics.setQueueSize(
			m.metrics.globalDependencyGraphInputTxBatchQueueSize,
			len(m.globalDepManager.incomingTransactionsNode),
		)
	}
}

// Close closes internal channels.
func (m *Manager) Close() {
	close(m.outgoingTxsNodeWithLocalDep)
}
