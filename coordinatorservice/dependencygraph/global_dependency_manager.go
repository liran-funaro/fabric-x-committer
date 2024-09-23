package dependencygraph

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"golang.org/x/sync/errgroup"
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

func (dm *globalDependencyManager) run(ctx context.Context) {
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		dm.constructDependencyGraph(gCtx)
		return nil
	})

	g.Go(func() error {
		dm.processValidatedTransactions(gCtx)
		return nil
	})

	g.Go(func() error {
		dm.outputFreedExistingTransactions(gCtx)
		return nil
	})

	_ = g.Wait()
}

func (dm *globalDependencyManager) constructDependencyGraph(ctx context.Context) {
	m := dm.metrics
	var txsNode []*TransactionNode
	incomingTransactionsNode := channel.NewReader(ctx, dm.incomingTransactionsNode)
	for {
		txsNodeBatch, ok := incomingTransactionsNode.Read()
		if !ok {
			return
		}

		constructionStart := time.Now()

		txsNode = txsNodeBatch.txsNode
		dm.waitingTxsSlots.acquire(uint32(len(txsNode)))

		m.setQueueSize(
			m.gdgWaitingTxQueueSize,
			int(int64(dm.waitingTxsLimit)-dm.waitingTxsSlots.availableSlots.Load()),
		)

		depFreeTxs := make([]*TransactionNode, 0, len(txsNode))

		start := time.Now()
		dm.mu.Lock()
		m.observe(m.gdgConstructorWaitForLockSeconds, time.Since(start))

		// Step 1: Detect dependencies of each transaction with the transactions
		//         that are already in the dependency graph. After detection,
		//         the dependencies are added to the transactionNode. If the
		//         transaction has no dependencies, it is added to the depFreeTxs
		//         slice.
		start = time.Now()
		for _, txNode := range txsNode {
			dependsOnTx := dm.dependencyDetector.getDependenciesOf(txNode)
			if len(dependsOnTx) > 0 {
				txNode.addDependenciesAndUpdateDependents(dependsOnTx)
			} else if len(txNode.dependsOnTxs) == 0 {
				depFreeTxs = append(depFreeTxs, txNode)
			}
		}
		m.observe(m.gdgAddTxToGraphSeconds, time.Since(start))

		// Step 2: Add reads and writes of each input transaction to the global dependency
		// 	       detector so that they can be used to detect correct dependencies
		// 		   for future input transactions. As the local dependency detector
		//         already has the reads and writes in required format, we are just
		// 	       merging it with the global dependency detector.
		start = time.Now()
		dm.dependencyDetector.mergeWaitingTx(txsNodeBatch.localDepDetector)
		m.observe(m.gdgUpdateDependencyDetectorSeconds, time.Since(start))

		dm.mu.Unlock()

		// Step 3: Send the transactions that are free of dependencies to the
		// 	       output channel.
		if len(depFreeTxs) > 0 {
			dm.outgoingDepFreeTransactionsNode <- depFreeTxs
		}
		m.addToCounter(m.gdgTxProcessedTotal, len(txsNode))
		m.observe(m.gdgConstructionSeconds, time.Since(constructionStart))
	}
}

func (dm *globalDependencyManager) processValidatedTransactions(ctx context.Context) {
	m := dm.metrics
	var fullyFreedDependents []*TransactionNode
	validatedTransactionsNode := channel.NewReader(ctx, dm.validatedTransactionsNode)
	for {
		txsNode, ok := validatedTransactionsNode.Read()
		if !ok {
			return
		}
		processValidatedStart := time.Now()
		dm.waitingTxsSlots.release(uint32(len(txsNode)))
		m.setQueueSize(
			m.gdgWaitingTxQueueSize,
			int(int64(dm.waitingTxsLimit)-dm.waitingTxsSlots.availableSlots.Load()),
		)

		start := time.Now()
		dm.mu.Lock()
		m.observe(m.gdgValidatedTxProcessorWaitForLockSeconds, time.Since(start))

		// Step 1: Remove the validated transactions from the dependency graph.
		//         When a transaction becomes free of dependencies, it is added
		// 	       to the fullyFreedDependents list so that it can be sent to
		//         the outgoingDepFreeTransactionsNode.
		start = time.Now()
		for _, txNode := range txsNode {
			fullyFreedDependents = append(fullyFreedDependents, txNode.freeDependents()...)
		}
		dm.dependencyDetector.removeWaitingTx(txsNode)
		m.observe(m.gdgRemoveDependentsOfValidatedTxSeconds, time.Since(start))
		dm.mu.Unlock()

		// Step 2: Send the fullyFreedDependents to the outgoingDepFreeTransactionsNode.
		start = time.Now()
		if len(fullyFreedDependents) > 0 {
			dm.freedTransactionsSet.add(fullyFreedDependents)
			fullyFreedDependents = nil
		}
		m.observe(m.gdgAddFreedTxSeconds, time.Since(start))

		dm.metrics.addToCounter(dm.metrics.gdgValidatedTxProcessedTotal, len(txsNode))
		m.observe(m.gdgValidatedTxProcessingSeconds, time.Since(processValidatedStart))
	}
}

func (dm *globalDependencyManager) outputFreedExistingTransactions(ctx context.Context) {
	stop := context.AfterFunc(ctx, func() {
		dm.freedTransactionsSet.nonEmpty.Store(true)
		dm.freedTransactionsSet.cond.Signal()
	})
	defer stop()

	for ctx.Err() == nil {
		start := time.Now()
		txsNode := dm.freedTransactionsSet.waitAndRemove()

		if len(txsNode) > 0 {
			dm.outgoingDepFreeTransactionsNode <- txsNode
		}
		dm.metrics.observe(dm.metrics.gdgOutputFreedTxSeconds, time.Since(start))
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
