/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package dependencygraph

import (
	"context"
	"sync"

	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring/promutil"
)

type (
	// SimpleManager is the simpler version of the dependency graph module.
	// It uses only 3 go routines, and a single regular map.
	SimpleManager struct {
		in                              <-chan *TransactionBatch
		out                             chan<- TxNodeBatch
		val                             <-chan TxNodeBatch
		waitingTxsLimit                 int
		preProcessedTxBatchQueue        chan TxNodeBatch
		preProcessedValidatedBatchQueue chan validatedBatch
		keyToWaitingTXs                 map[string]*waiting
		// depFreeTxBatches holds released batches that the output channel has not taken yet.
		// It is deliberately unbounded: taskProcessing must never block on the output while a
		// validated batch waits for it. See the cycle described in taskProcessing.
		depFreeTxBatches []TxNodeBatch
		waitingTXs       int
		metrics          *perfMetrics
	}

	validatedBatch struct {
		txCount int
		waiting []*waiting
	}

	// waiting tracks the transactions that use one key: the group that is allowed to run, and the
	// groups queued behind it. The running group is kept as a count rather than as its members
	// because those members have already been released and nothing here needs them again. Holding
	// them would retain every transaction that ever touched a continuously read key -- the
	// namespace key that every transaction reads is exactly that -- for the lifetime of the load.
	waiting struct {
		key string
		// runningCount is the number of transactions in the running group that are not validated
		// yet. The key is free once it reaches zero.
		runningCount int
		// runningIsWriter records whether the running group is a writer, since a reader may join
		// only a group of readers.
		runningIsWriter bool
		queue           []*waiterGroup
	}

	waiterGroup struct {
		group  []*TransactionNode
		writer bool
	}
)

// NewSimpleManager create a simple dependency graph manager.
func NewSimpleManager(p *Parameters) *SimpleManager {
	return &SimpleManager{
		in:                              p.IncomingTxs,
		out:                             p.OutgoingDepFreeTxsNode,
		val:                             p.IncomingValidatedTxsNode,
		waitingTxsLimit:                 p.WaitingTxsLimit,
		preProcessedTxBatchQueue:        make(chan TxNodeBatch, cap(p.IncomingTxs)),
		preProcessedValidatedBatchQueue: make(chan validatedBatch, cap(p.IncomingValidatedTxsNode)),
		keyToWaitingTXs:                 make(map[string]*waiting),
		metrics:                         newPerformanceMetrics(p.PrometheusMetricsProvider),
	}
}

// Run starts the dependency graph manager.
func (m *SimpleManager) Run(ctx context.Context) {
	wg := sync.WaitGroup{}
	wg.Add(3)
	go func() {
		// This manager must have a single incoming pre-processing
		// worker to maintain the original TX order.
		defer wg.Done()
		m.preProcessIn(ctx)
	}()
	go func() {
		defer wg.Done()
		m.preProcessVal(ctx)
	}()
	go func() {
		defer wg.Done()
		m.taskProcessing(ctx)
	}()
	wg.Wait()
}

// preProcessIn maps the data.
// - in  (TransactionBatch) -> preProcessedTxBatchQueue (TxNodeBatch).
func (m *SimpleManager) preProcessIn(ctx context.Context) {
	in := channel.NewReader(ctx, m.in)
	batchQueue := channel.NewWriter(ctx, m.preProcessedTxBatchQueue)
	for ctx.Err() == nil {
		batch, ok := in.Read()
		if !ok {
			return
		}
		depTX := make([]*TransactionNode, len(batch.Txs))
		for i, tx := range batch.Txs {
			node := newTransactionNode(tx)
			node.waitingKeys = make([]*waiting, 0, node.rwKeys.size())
			depTX[i] = node
		}
		batchQueue.Write(depTX)
	}
}

// preProcessVal maps the data.
// - val (TxNodeBatch)      -> preProcessedValidatedBatchQueue   (validated).
func (m *SimpleManager) preProcessVal(ctx context.Context) {
	val := channel.NewReader(ctx, m.val)
	valQueue := channel.NewWriter(ctx, m.preProcessedValidatedBatchQueue)
	for ctx.Err() == nil {
		batch, ok := val.Read()
		if !ok {
			return
		}
		var ws []*waiting
		txCount := 0
		for _, node := range batch {
			if !node.inDependencyGraph {
				continue
			}
			txCount++
			ws = append(ws, node.waitingKeys...)
		}
		if txCount == 0 {
			continue
		}
		valQueue.Write(validatedBatch{
			txCount: txCount,
			waiting: ws,
		})
	}
}

// taskProcessing -- taskQueue (TxNodeBatch/keys) -> out (TxNodeBatch).
func (m *SimpleManager) taskProcessing(ctx context.Context) {
	for ctx.Err() == nil {
		batchQueue := m.preProcessedTxBatchQueue
		if m.waitingTXs > m.waitingTxsLimit {
			// When we passed the waiting TX limit, we only fetch from the validation queue.
			batchQueue = nil
		}

		// The output is a case of the select rather than a blocking write after it, because this
		// goroutine is also the only consumer of the validated queue. Blocking on a full output
		// while a validated batch waits closes a cycle: the verifiers and the vcservices stop
		// consuming our output once their own results have nowhere to go, so the output never
		// drains again and the pipeline halts with nothing logged anywhere. A nil channel blocks
		// forever, which is how the case is disabled when we have nothing to send.
		var outQueue chan<- TxNodeBatch
		var outBatch TxNodeBatch
		if len(m.depFreeTxBatches) > 0 {
			outQueue, outBatch = m.out, m.depFreeTxBatches[0]
		}

		var depFree TxNodeBatch
		select {
		case <-ctx.Done():
			return
		case outQueue <- outBatch:
			m.depFreeTxBatches = m.depFreeTxBatches[1:]
			continue
		case batch := <-batchQueue:
			depFree = m.processTxBatch(batch)
			promutil.AddToCounter(m.metrics.gdgTxProcessedTotal, len(batch))
			promutil.AddToGauge(m.metrics.dependentTransactionsQueueSize, len(batch)-len(depFree))
		case batch := <-m.preProcessedValidatedBatchQueue:
			depFree = m.processValidatedBatch(batch)
			promutil.AddToCounter(m.metrics.gdgValidatedTxProcessedTotal, batch.txCount)
			promutil.SubFromGauge(m.metrics.dependentTransactionsQueueSize, len(depFree))
		}
		promutil.SetGauge(m.metrics.gdgWaitingTxQueueSize, m.waitingTXs)
		if len(depFree) > 0 {
			m.depFreeTxBatches = append(m.depFreeTxBatches, depFree)
		}
	}
}

func (m *SimpleManager) processTxBatch(batch TxNodeBatch) TxNodeBatch {
	m.waitingTXs += len(batch)
	depFree := make(TxNodeBatch, 0, len(batch))
	for _, depTX := range batch {
		// With writes.
		for _, rw := range [][]string{depTX.rwKeys.readsAndWrites, depTX.rwKeys.writesOnly} {
			for _, k := range rw {
				m.checkTXFree(depTX, k, true)
			}
		}
		// Read only.
		for _, k := range depTX.rwKeys.readsOnly {
			m.checkTXFree(depTX, k, false)
		}

		if depTX.waitForKeysCount == 0 {
			depFree = append(depFree, depTX)
		}
	}
	return depFree
}

func (m *SimpleManager) processValidatedBatch(val validatedBatch) TxNodeBatch {
	m.waitingTXs -= val.txCount
	depFree := make(TxNodeBatch, 0, len(val.waiting))
	for _, w := range val.waiting {
		depFree = m.appendFree(depFree, w)
	}
	return depFree
}

// checkTXFree check there are active TXs that are using a key, and if so, it adds the TX to thw wait queue.
//
//nolint:revive // false positive: control flag.
func (m *SimpleManager) checkTXFree(tx *TransactionNode, k string, writer bool) {
	w, loaded := m.keyToWaitingTXs[k]
	if loaded {
		if w.add(tx, writer) {
			tx.waitForKeysCount++
		}
	} else {
		w = &waiting{
			key:             k,
			runningCount:    1,
			runningIsWriter: writer,
		}
		m.keyToWaitingTXs[k] = w
	}
	tx.waitingKeys = append(tx.waitingKeys, w)
}

// appendFree indicate a key processing item is done, and appends free nodes if applicable.
func (m *SimpleManager) appendFree(out TxNodeBatch, w *waiting) TxNodeBatch {
	nextWaiters, noMoreWait := w.popAndGetNext()
	if noMoreWait {
		delete(m.keyToWaitingTXs, w.key)
		return out
	}

	for _, txWait := range nextWaiters {
		txWait.waitForKeysCount--
		if txWait.waitForKeysCount == 0 {
			out = append(out, txWait)
		}
	}
	return out
}

// add append to the waiting queue.
// returns true if the wait is needed.
func (w *waiting) add(tx *TransactionNode, writer bool) bool { //nolint:revive // false positive: control flag.
	sz := len(w.queue)
	if sz == 0 {
		// A reader can join a running group of readers and proceed with it.
		if !writer && !w.runningIsWriter {
			w.runningCount++
			return false
		}
		w.queue = append(w.queue, &waiterGroup{group: []*TransactionNode{tx}, writer: writer})
		return true
	}

	// When this item or the last queued item is a writer, we should add a new group.
	// Otherwise, the latest group are readers and this item is also a reader, so we can append it.
	last := w.queue[sz-1]
	if writer || last.writer {
		w.queue = append(w.queue, &waiterGroup{group: []*TransactionNode{tx}, writer: writer})
	} else {
		last.group = append(last.group, tx)
	}
	return true
}

// popAndGetNext removes a TX from the waiters.
// Returns the next waiter group to release.
// Returns true if no other TX is waiting.
func (w *waiting) popAndGetNext() ([]*TransactionNode, bool) {
	w.runningCount--
	if w.runningCount > 0 {
		return nil, false
	}
	if len(w.queue) == 0 {
		return nil, true
	}

	next := w.queue[0]
	w.queue = w.queue[1:]
	w.runningCount = len(next.group)
	w.runningIsWriter = next.writer
	return next.group, false
}
