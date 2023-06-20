package pipeline

import (
	"sync"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/protos/token"
	"go.opentelemetry.io/otel/attribute"
)

type dependencyMgr struct {
	inputChan              chan *token.Block
	inputChanStatusUpdate  chan []*TxStatus
	outputChanStatusUpdate chan []*TxStatus

	c             *sync.Cond
	numBlocksSeen uint64
	snToNodes     map[string]map[*node]struct{}
	nodes         map[TxSeqNum]*node

	stopSignalCh                 chan struct{}
	maxGraphSize                 int
	dependencyGraphUpdateTimeout time.Duration
	metrics                      *metrics.Metrics
}

type node struct {
	txID          *TxSeqNum
	serialNumbers [][]byte
	dependents    map[*node]struct{}
	dependsOn     map[*node]struct{}
}

func newDependencyMgr(maxGraphSize int, dependencyGraphUpdateTimeout time.Duration, metrics *metrics.Metrics) *dependencyMgr {
	m := &dependencyMgr{
		inputChan:                    make(chan *token.Block, defaultChannelBufferSize),
		inputChanStatusUpdate:        make(chan []*TxStatus, defaultChannelBufferSize),
		c:                            sync.NewCond(&sync.Mutex{}),
		snToNodes:                    map[string]map[*node]struct{}{},
		nodes:                        map[TxSeqNum]*node{},
		stopSignalCh:                 make(chan struct{}),
		outputChanStatusUpdate:       make(chan []*TxStatus, defaultChannelBufferSize),
		maxGraphSize:                 maxGraphSize,
		dependencyGraphUpdateTimeout: dependencyGraphUpdateTimeout,
		metrics:                      metrics,
	}

	if metrics.Enabled {
		m.metrics.DependencyMgrInputChLength.SetCapacity(defaultChannelBufferSize)
		m.metrics.DependencyMgrStatusUpdateChLength.SetCapacity(defaultChannelBufferSize)
		m.metrics.DependencyMgrOutputChLength.SetCapacity(defaultChannelBufferSize)
	}

	m.startBlockRecieverRoutine()
	m.startStatusUpdateProcessorRoutine()
	return m
}

func (m *dependencyMgr) startBlockRecieverRoutine() {
	go func() {
		for {
			select {
			case <-m.stopSignalCh:
				return
			case b := <-m.inputChan:
				m.updateGraphWithNewBlock(b)
				if m.metrics.Enabled {
					m.metrics.DependencyMgrInTxs.Add(len(b.Txs))
					m.metrics.DependencyMgrInputChLength.Set(len(m.inputChan))
				}
			}
		}
	}()
}

func (m *dependencyMgr) updateGraphWithNewBlock(b *token.Block) {
	beforeLock := time.Now()
	m.c.L.Lock()
	unlocked := time.Now()
	defer m.c.L.Unlock()

	for len(m.snToNodes) >= m.maxGraphSize {
		m.c.Wait()
	}

	for i, tx := range b.Txs {
		blkNumTxNum := TxSeqNum{
			BlkNum: b.Number,
			TxNum:  uint64(i),
		}

		newNode := &node{
			txID:          &blkNumTxNum,
			serialNumbers: tx.SerialNumbers,
			dependents:    map[*node]struct{}{},
			dependsOn:     map[*node]struct{}{},
		}
		m.nodes[blkNumTxNum] = newNode

		for _, sn := range tx.SerialNumbers {
			existingNodes, ok := m.snToNodes[string(sn)]
			if !ok {
				existingNodes = map[*node]struct{}{}
			}

			for en := range existingNodes {
				newNode.dependsOn[en] = struct{}{}
				en.dependents[newNode] = struct{}{}
			}

			existingNodes[newNode] = struct{}{}
			if !ok {
				m.snToNodes[string(sn)] = existingNodes
			}
		}
		if m.metrics.Enabled {
			m.metrics.RequestTracer.AddEventAt(blkNumTxNum, "Waiting to add TX to graph (lock)", beforeLock)
			m.metrics.RequestTracer.AddEventAt(blkNumTxNum, "Adding TX to graph", unlocked)
			m.metrics.RequestTracer.AddEvent(blkNumTxNum, "Added TX to graph")
		}
	}

	m.numBlocksSeen = b.Number + 1

	if m.metrics.Enabled {
		m.metrics.DependencyGraphPendingSNs.Set(float64(len(m.snToNodes)))
		m.metrics.DependencyGraphPendingTXs.Set(float64(len(m.nodes)))
	}
}

func (m *dependencyMgr) startStatusUpdateProcessorRoutine() {
	notYetSeenTxs := []*TxStatus{}
	go func() {
		for {
			select {
			case <-m.stopSignalCh:
				return

			case u := <-m.inputChanStatusUpdate:
				notYetSeenTxs = m.updateGraphWithValidatedTxs(append(u, notYetSeenTxs...))
				if m.metrics.Enabled {
					m.metrics.DependencyMgrStatusUpdateChLength.Set(len(m.inputChanStatusUpdate))
				}

			case <-time.After(m.dependencyGraphUpdateTimeout):
				if len(notYetSeenTxs) > 0 {
					notYetSeenTxs = m.updateGraphWithValidatedTxs(notYetSeenTxs)
				}
			}
		}
	}()
}

func (m *dependencyMgr) fetchDependencyFreeTxsThatIntersect(enquirySet []TxSeqNum) (map[TxSeqNum][][]byte, []TxSeqNum) {
	beforeLock := time.Now()
	m.c.L.Lock()
	unlocked := time.Now()
	defer m.c.L.Unlock()

	dependencyFreeTxs := make(map[TxSeqNum][][]byte, len(enquirySet))
	dependentOrNotYetSeenTxs := []TxSeqNum{}

	for _, e := range enquirySet {
		node, ok := m.nodes[e]
		if ok && len(node.dependsOn) == 0 {
			dependencyFreeTxs[e] = node.serialNumbers
		} else if !ok && m.hasSeen(e.BlkNum) {
			// This can happen only if the transaction is already invalidated by dependency graph because one of its dependency got validated
		} else {
			dependentOrNotYetSeenTxs = append(dependentOrNotYetSeenTxs, e)
		}

		if ok && m.metrics.Enabled {
			m.metrics.RequestTracer.AddEventAt(e, "Waiting to query graph (lock)", beforeLock)
			m.metrics.RequestTracer.AddEventAt(e, "Querying graph", unlocked)
			m.metrics.RequestTracer.AddEvent(e, "Finished querying graph")
		}
	}
	return dependencyFreeTxs, dependentOrNotYetSeenTxs
}

func (m *dependencyMgr) updateGraphWithValidatedTxs(toUpdate []*TxStatus) []*TxStatus {
	beforeLock := time.Now()
	m.c.L.Lock()
	unlocked := time.Now()
	defer func() {
		m.c.Signal()
		m.c.L.Unlock()
	}()

	notYetSeenTxs := []*TxStatus{}
	processedTxs := make([]*TxStatus, 0, len(toUpdate))
	cascadeInvalidatedTxs := map[TxSeqNum]struct{}{}

	for _, u := range toUpdate {
		node, ok := m.nodes[u.TxSeqNum]

		if !ok {
			// This can happen only if sigverifier invalidates the transaction
			if m.hasSeen(u.TxSeqNum.BlkNum) {
				// This can happen only if the transaction is already invalidated by dependency graph because one of its dependency got validated
				continue
			}
			notYetSeenTxs = append(notYetSeenTxs, u)
			continue
		}

		processedTxs = append(processedTxs, u)
		before := time.Now()
		m.removeNodeUnderAcquiredLock(node, u.Status == VALID, cascadeInvalidatedTxs)
		if ok && m.metrics.Enabled {
			m.metrics.RequestTracer.AddEventAt(u.TxSeqNum, "Waiting to remove TX from graph (lock)", beforeLock)
			m.metrics.RequestTracer.AddEventAt(u.TxSeqNum, "Removing TX from graph", unlocked)
			m.metrics.RequestTracer.AddEventAt(u.TxSeqNum, "Starting removal of TX from dependency graph", before)
			m.metrics.RequestTracer.AddEvent(u.TxSeqNum, "Removed TX from dependency graph")
		}
	}

	for k := range cascadeInvalidatedTxs {
		processedTxs = append(processedTxs,
			&TxStatus{
				TxSeqNum: k,
				Status:   DOUBLE_SPEND,
			},
		)
	}

	if len(processedTxs) > 0 {
		m.outputChanStatusUpdate <- processedTxs
	}

	if m.metrics.Enabled {
		sent := time.Now()
		for _, status := range processedTxs {
			m.metrics.RequestTracer.EndAt(status.TxSeqNum, sent, attribute.String(metrics.StatusLabel, status.Status.String()))
		}
		m.metrics.DependencyGraphPendingSNs.Set(float64(len(m.snToNodes)))
		m.metrics.DependencyGraphPendingTXs.Set(float64(len(m.nodes)))
		m.metrics.DependencyMgrOutputChLength.Set(len(m.outputChanStatusUpdate))
	}
	return notYetSeenTxs
}

func (m *dependencyMgr) hasSeen(blockNum uint64) bool {
	return blockNum < m.numBlocksSeen
}

func (m *dependencyMgr) removeNodeUnderAcquiredLock(node *node, validTx bool, cascadeInvalidatedTxs map[TxSeqNum]struct{}) {
	delete(m.nodes, *node.txID)

	for _, sn := range node.serialNumbers {
		snKey := string(sn)
		nodesAgainstSN := m.snToNodes[snKey]
		if len(nodesAgainstSN) == 1 {
			delete(m.snToNodes, snKey)
		} else {
			delete(nodesAgainstSN, node)
		}
	}

	if !validTx {
		// We can take this to next level by taking the cause of invalidation. If the cause of invalidation is serial number
		// clash with an already committed transaction, we can cascade invalidation to some of the dependent nodes.
		// However, that would add additional overheads at the lower level layers as a transaction would have to wait to collect the results
		// for all the serial numbers and the status needs to be fine grained at the serial number level from the db layer.
		// As we do not anticipate much conflcits in the transactions, we leave the dependent transactions to be evaluated
		// later independently for now.
		for d := range node.dependsOn {
			delete(d.dependents, node)
		}

		for d := range node.dependents {
			delete(d.dependsOn, node)
		}
		return
	}

	for d := range node.dependents {
		cascadeInvalidatedTxs[*d.txID] = struct{}{}
		m.removeNodeUnderAcquiredLock(d, false, cascadeInvalidatedTxs)
	}
}

func (m *dependencyMgr) stop() {
	close(m.stopSignalCh)
	close(m.outputChanStatusUpdate)
}
