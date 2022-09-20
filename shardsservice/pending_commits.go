package shardsservice

import sync "sync"

type pendingCommits struct {
	txIDsToSerialNumbers map[txID]*SerialNumbers
	mu                   sync.RWMutex
}

func (p *pendingCommits) add(tx txID, sNumbers *SerialNumbers) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.txIDsToSerialNumbers[tx] = sNumbers
}

func (p *pendingCommits) get(tx txID) *SerialNumbers {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.txIDsToSerialNumbers[tx]
}

func (p *pendingCommits) count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.txIDsToSerialNumbers)
}

func (p *pendingCommits) delete(tx txID) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.txIDsToSerialNumbers, tx)
}

func (p *pendingCommits) deleteAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for txID := range p.txIDsToSerialNumbers {
		delete(p.txIDsToSerialNumbers, txID)
	}
}
