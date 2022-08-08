package pipeline

import (
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

const maxSerialNumbersEntries = 1000000

type dependencyMgr struct {
	c         *sync.Cond
	snToNodes map[string]map[*node]struct{}
	nodes     map[blkNumTxNum]*node
}

type node struct {
	blkNumTxNum   *blkNumTxNum
	serialNumbers [][]byte
	dependents    map[*node]struct{}
	dependsOn     map[*node]struct{}
}

func newDependencyMgr() *dependencyMgr {
	return &dependencyMgr{
		c:         sync.NewCond(&sync.Mutex{}),
		snToNodes: map[string]map[*node]struct{}{},
		nodes:     map[blkNumTxNum]*node{},
	}
}

func (m *dependencyMgr) processBlock(block *token.Block) {
	m.c.L.Lock()
	defer m.c.L.Unlock()

	for !(len(m.snToNodes) < maxSerialNumbersEntries) {
		m.c.Wait()
	}

	for i, tx := range block.Txs {
		blkNumTxNum := blkNumTxNum{
			blkNum: block.Number,
			txNum:  uint64(i),
		}

		newNode := &node{
			blkNumTxNum:   &blkNumTxNum,
			serialNumbers: tx.SerialNumbers,
			dependents:    map[*node]struct{}{},
			dependsOn:     map[*node]struct{}{},
		}
		m.nodes[blkNumTxNum] = newNode

		for _, sn := range tx.SerialNumbers {
			existingNodes := m.snToNodes[string(sn)]
			for en := range existingNodes {
				newNode.dependsOn[en] = struct{}{}
				en.dependents[newNode] = struct{}{}
			}

			n, ok := m.snToNodes[string(sn)]
			if !ok {
				n = map[*node]struct{}{}
				m.snToNodes[string(sn)] = n
			}
			n[newNode] = struct{}{}
		}
	}
}

func (m *dependencyMgr) fetchDependencyFreeTxsThatIntersect(enquirySet []blkNumTxNum) (map[blkNumTxNum][][]byte, []blkNumTxNum) {
	m.c.L.Lock()
	defer m.c.L.Unlock()

	dependencyFreeTxs := map[blkNumTxNum][][]byte{}
	dependentOrNotYetSeenTxs := []blkNumTxNum{}

	for _, e := range enquirySet {
		node, ok := m.nodes[e]
		if ok && len(node.dependsOn) == 0 {
			dependencyFreeTxs[e] = node.serialNumbers
		} else {
			dependentOrNotYetSeenTxs = append(dependentOrNotYetSeenTxs, e)
		}
	}
	return dependencyFreeTxs, dependentOrNotYetSeenTxs
}

func (m *dependencyMgr) processValidatedTxs(toUpdate []*txValidationStatus) []*txValidationStatus {
	m.c.L.Lock()
	defer func() {
		m.c.Signal()
		m.c.L.Unlock()
	}()

	notYetSeenTxs := []*txValidationStatus{}
	for _, u := range toUpdate {
		node, ok := m.nodes[u.blkNumTxNum]
		if !ok {
			// This can happen only when sigverifier invalidates the transaction
			notYetSeenTxs = append(notYetSeenTxs, u)
			continue
		}
		m.removeNodeUnderAcquiredLock(node, u.isValid)
	}
	return notYetSeenTxs
}

func (m *dependencyMgr) removeNodeUnderAcquiredLock(node *node, validTx bool) {
	delete(m.nodes, *node.blkNumTxNum)
	for _, sn := range node.serialNumbers {
		delete(m.snToNodes[string(sn)], node)
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
		m.removeNodeUnderAcquiredLock(d, false)
	}
}
