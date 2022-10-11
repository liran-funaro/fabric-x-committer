package pendingcommits

import (
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"go.uber.org/atomic"
)

type condPendingCommits struct {
	txIdToSnMap   map[TxID][]token.SerialNumber
	snToChMap     map[string]chan struct{}
	c             *sync.Cond
	maxBufferSize int
	totalTxs      atomic.Uint64
	totalSNs      atomic.Uint64
}

func NewCondPendingCommits(maxBufferSize uint32) PendingCommits {
	return &condPendingCommits{
		c:             sync.NewCond(&sync.RWMutex{}),
		maxBufferSize: int(maxBufferSize),
		txIdToSnMap:   make(map[TxID][]token.SerialNumber),
		snToChMap:     make(map[string]chan struct{}),
	}
}

func (p *condPendingCommits) Add(tx TxID, serialNumbers []token.SerialNumber) {
	p.c.L.Lock()
	defer p.c.L.Unlock()
	for len(p.txIdToSnMap) >= p.maxBufferSize {
		p.c.Wait()
	}
	p.txIdToSnMap[tx] = serialNumbers
	for _, serialNumber := range serialNumbers {
		p.snToChMap[string(serialNumber)] = make(chan struct{})
	}

	p.totalTxs.Inc()
	p.totalSNs.Add(uint64(len(serialNumbers)))
}

func (p *condPendingCommits) Delete(tx TxID) {
	p.c.L.Lock()
	deletedSNs := p.deleteInternal(tx)
	p.c.Signal()
	p.c.L.Unlock()
	p.totalTxs.Dec()
	p.totalSNs.Sub(uint64(deletedSNs))
}

func (p *condPendingCommits) DeleteBatch(txIds []TxID) {
	p.c.L.Lock()
	deletedSNs := 0
	for _, txId := range txIds {
		deletedSNs += p.deleteInternal(txId)
	}

	p.c.Broadcast()
	p.c.L.Unlock()
	p.totalTxs.Sub(uint64(len(txIds)))
	p.totalSNs.Sub(uint64(deletedSNs))
}

func (p *condPendingCommits) deleteInternal(tx TxID) int {
	serialNumbers, _ := p.txIdToSnMap[tx]
	delete(p.txIdToSnMap, tx)
	for _, serialNumber := range serialNumbers {
		ch, _ := p.snToChMap[string(serialNumber)]
		close(ch)
		delete(p.snToChMap, string(serialNumber))
	}
	return len(serialNumbers)
}

func (p *condPendingCommits) Get(tx TxID) []token.SerialNumber {
	p.c.L.(*sync.RWMutex).RLock()
	defer p.c.L.(*sync.RWMutex).RUnlock()
	return p.txIdToSnMap[tx]
}

func (p *condPendingCommits) WaitTillNotExist(serialNumbers []token.SerialNumber) {
	waits := make([]chan struct{}, 0, len(serialNumbers))
	p.c.L.(*sync.RWMutex).RLock()
	for _, serialNumber := range serialNumbers {
		if ch, ok := p.snToChMap[string(serialNumber)]; ok {
			logger.Debugf("waiting for serial number [%s]", string(serialNumber))
			waits = append(waits, ch)
		}
	}
	p.c.L.(*sync.RWMutex).RUnlock()
	for _, wait := range waits {
		<-wait
	}
}

func (p *condPendingCommits) CountTxs() int {
	return int(p.totalTxs.Load())
}

func (p *condPendingCommits) CountSNs() int {
	return int(p.totalSNs.Load())
}

func (p *condPendingCommits) DeleteAll() {
	p.c.L.Lock()
	defer p.c.L.Unlock()
	p.txIdToSnMap = make(map[TxID][]token.SerialNumber)
	for _, ch := range p.snToChMap {
		close(ch)
	}
	p.snToChMap = make(map[string]chan struct{})
	p.totalTxs.Swap(0)
	p.totalSNs.Swap(0)
}
