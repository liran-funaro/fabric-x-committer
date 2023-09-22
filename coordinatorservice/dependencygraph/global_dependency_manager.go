package dependencygraph

import (
	"sync"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/workerpool"
)

type (
	globalDependencyManager struct {
		// incomingTransactionsNode is one of the two inputs to the globalDependencyManager.
		// These transactions are added to the global dependency graph.
		// The local dependency detector sends transactions node on this channel.
		incomingTransactionsNode <-chan *transactionNodeBatch

		// outgoingDepFreeTransactionsNode is the output of the globalDependencyManager.
		// These transactions are ready to be validated as they are dependency free.
		outgoingDepFreeTransactionsNode chan<- []*TransactionNode

		// validatedTransactionsNode is the second input to the dependencyManagement.
		// These transactions are removed from the dependency graph as they are
		// validated and either committed or aborted.
		validatedTransactionsNode <-chan []*TransactionNode

		// TODO: add a text figure explaining how the input and output channels
		//       are being used.

		// dependencyDetector holds mapping between reads/writes and transactions.
		dependencyDetector *dependencyDetector

		// workerPool is used to process transactions in parallel. This workerPool is
		// currently used only during the construction of the dependency graph.
		workerPool *workerpool.WorkerPool

		mu sync.Mutex

		// The freedTransactionsSet holds transactions that have been released from the dependency
		// graph. They are added to this set once the pending transactions on which they depend
		// have been validated. The transactions in this set are sent on the
		// outgoingDepFreeTransactionsNode channel.
		freedTransactionsSet *dependencyFreedTransactions

		// waitingTxsSlots is employed to restrict the number of transactions awaiting validation.
		// This slot helps prevent the dependency graph from becoming too extensive. The number of
		// transactions within transactionNodeBatch (an input for the globalDependencyManager)
		// must be lower than the maximum allowed number of slots. This is not guaranteed by the
		// localDependencyConstructor, but rather by the input provided to it. Typically, the
		// maximum allowed number of slots would be considerably larger than the batch size.
		waitingTxsSlots *waitingTransactionsSlots
		waitingTxsLimit int

		metrics *perfMetrics
	}

	dependencyFreedTransactions struct {
		txsNode  []*TransactionNode
		mu       sync.Mutex
		nonEmpty *atomic.Bool
		cond     *sync.Cond
	}

	waitingTransactionsSlots struct {
		availableSlots *atomic.Int64
		slotsCond      *sync.Cond
	}

	globalDepConfig struct {
		incomingTxsNode        <-chan *transactionNodeBatch
		outgoingDepFreeTxsNode chan<- []*TransactionNode
		validatedTxsNode       <-chan []*TransactionNode
		workerPoolConfig       *workerpool.Config
		waitingTxsLimit        int
		metrics                *perfMetrics
	}
)

func newGlobalDependencyManager(c *globalDepConfig) *globalDependencyManager {
	slotsForWaitingTxs := &atomic.Int64{}
	slotsForWaitingTxs.Store(int64(c.waitingTxsLimit))

	return &globalDependencyManager{
		incomingTransactionsNode:        c.incomingTxsNode,
		outgoingDepFreeTransactionsNode: c.outgoingDepFreeTxsNode,
		validatedTransactionsNode:       c.validatedTxsNode,
		dependencyDetector:              newDependencyDetector(),
		workerPool:                      workerpool.New(c.workerPoolConfig),
		mu:                              sync.Mutex{},
		freedTransactionsSet: &dependencyFreedTransactions{
			txsNode:  []*TransactionNode{},
			mu:       sync.Mutex{},
			nonEmpty: &atomic.Bool{},
			cond:     sync.NewCond(&sync.Mutex{}),
		},
		waitingTxsSlots: &waitingTransactionsSlots{
			availableSlots: slotsForWaitingTxs,
			slotsCond:      sync.NewCond(&sync.Mutex{}),
		},
		waitingTxsLimit: c.waitingTxsLimit,
		metrics:         c.metrics,
	}
}

func (dm *globalDependencyManager) start() {
	go dm.constructDependencyGraph()
	go dm.processValidatedTransactions()
	go dm.outputFreedExistingTransactions()
}

func (dm *globalDependencyManager) constructDependencyGraph() {
	var wg sync.WaitGroup
	for txsNodeBatch := range dm.incomingTransactionsNode {
		txsNode := txsNodeBatch.txsNode

		dm.waitingTxsSlots.acquire(uint32(len(txsNode)))
		dm.metrics.setQueueSize(
			dm.metrics.globalDependencyGraphWaitingTxQueueSize,
			int(int64(dm.waitingTxsLimit)-dm.waitingTxsSlots.availableSlots.Load()),
		)

		wg.Add(len(txsNode))
		dm.mu.Lock()
		for _, txNode := range txsNode {
			// Step 1: Detect dependencies of each transaction with the transactions
			//         that are already in the dependency graph. This is done in
			// 	       parallel as we have already detected dependencies between
			//         transactions present in the input batch. After detection,
			//         the dependencies are added to the transactionNode.
			tNode := txNode
			findAndUpdateDep := func() {
				dependsOnTx := dm.dependencyDetector.getDependenciesOf(tNode)
				tNode.addDependenciesAndUpdateDependents(dependsOnTx)
				wg.Done()
			}
			dm.workerPool.Run(findAndUpdateDep)
		}
		wg.Wait()

		// Step 2: Add reads and writes of each input transaction to the global dependency
		// 	       detector so that they can be used to detect correct dependencies
		// 		   for future input transactions. As the local dependency detector
		//         already has the reads and writes in required format, we are just
		// 	       merging it with the global dependency detector.
		mergeLocalAndGlobalDetector := func() {
			dm.dependencyDetector.mergeWaitingTx(txsNodeBatch.localDepDetector)
			wg.Done()
		}
		wg.Add(1)
		dm.workerPool.Run(mergeLocalAndGlobalDetector)

		// Step 3: Find all the transactions that are dependency free and send
		// 	       them to the outgoingDepFreeTransactionsNode.
		depFreeTxs := make([]*TransactionNode, 0, len(txsNode))
		findFreeTxsAndSend := func() {
			for _, txNode := range txsNode {
				if txNode.isDependencyFree() {
					depFreeTxs = append(depFreeTxs, txNode)
				}
			}

			wg.Done()
		}
		wg.Add(1)
		dm.workerPool.Run(findFreeTxsAndSend)

		wg.Wait()

		dm.mu.Unlock()

		if len(depFreeTxs) > 0 {
			dm.outgoingDepFreeTransactionsNode <- depFreeTxs
		}

		dm.metrics.addToCounter(dm.metrics.globalDependencyGraphTransactionProcessedTotal, len(txsNode))
	}

	dm.workerPool.Close()
}

func (dm *globalDependencyManager) processValidatedTransactions() {
	for txsNode := range dm.validatedTransactionsNode {
		dm.waitingTxsSlots.release(uint32(len(txsNode)))
		dm.metrics.setQueueSize(
			dm.metrics.globalDependencyGraphWaitingTxQueueSize,
			int(int64(dm.waitingTxsLimit)-dm.waitingTxsSlots.availableSlots.Load()),
		)

		var fullyFreedDependents []*TransactionNode

		dm.mu.Lock()
		// Step 1: Remove the validated transactions from the dependency graph.
		//         When a transaction becomes free of dependencies, it is added
		// 	       to the fullyFreedDependents list so that it can be sent to
		//         the outgoingDepFreeTransactionsNode.
		for _, txNode := range txsNode {
			fullyFreedDependents = append(fullyFreedDependents, txNode.freeDependents()...)
			dm.dependencyDetector.removeWaitingTx(txNode)
		}
		dm.mu.Unlock()

		// Step 2: Send the fullyFreedDependents to the outgoingDepFreeTransactionsNode.
		if len(fullyFreedDependents) > 0 {
			dm.freedTransactionsSet.add(fullyFreedDependents)
		}

		dm.metrics.addToCounter(dm.metrics.globalDependencyGraphValidatedTransactionProcessedTotal, len(txsNode))
	}
}

func (dm *globalDependencyManager) outputFreedExistingTransactions() {
	for {
		txsNode := dm.freedTransactionsSet.waitAndRemove()

		if len(txsNode) > 0 {
			dm.outgoingDepFreeTransactionsNode <- txsNode
		}
	}
}

func (f *dependencyFreedTransactions) add(txsNode []*TransactionNode) {
	f.mu.Lock()
	f.txsNode = append(
		f.txsNode,
		txsNode...,
	)
	f.mu.Unlock()

	f.nonEmpty.Store(true)
	f.cond.Signal()
}

func (f *dependencyFreedTransactions) waitAndRemove() []*TransactionNode {
	f.cond.L.Lock()
	for !f.nonEmpty.CompareAndSwap(true, false) {
		f.cond.Wait()
	}
	f.cond.L.Unlock()

	// NOTE: We have changed the nonEmpty to false before fetching txsNode
	//       and making the list empty. By the time, we remove the txsNode
	//       from the list, the nonEmpty might have been set to true by
	//       processValidatedTransactions. As a result, the next call to
	//       waitAndRemove may return an empty list. This is fine as long
	//       as the caller is able to handle empty list.
	f.mu.Lock()
	txsNode := f.txsNode
	f.txsNode = nil
	f.mu.Unlock()

	return txsNode
}

func (w *waitingTransactionsSlots) acquire(n uint32) {
	w.slotsCond.L.Lock()
	for w.availableSlots.Load() < int64(n) {
		w.slotsCond.Wait()
	}
	w.availableSlots.Add(-int64(n))
	w.slotsCond.L.Unlock()
}

func (w *waitingTransactionsSlots) release(n uint32) {
	w.availableSlots.Add(int64(n))
	w.slotsCond.Signal()
}
