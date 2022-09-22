package shardsservice

import sync "sync"

type pendingCommits struct {
	txIDsToSerialNumbers map[txID]*SerialNumbers
	serialNumbers        map[string]struct{}
	mu                   sync.RWMutex
}

func (p *pendingCommits) add(tx txID, sNumbers *SerialNumbers) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.txIDsToSerialNumbers[tx] = sNumbers
	for _, sn := range sNumbers.SerialNumbers {
		p.serialNumbers[string(sn)] = struct{}{}
	}
}

func (p *pendingCommits) get(tx txID) *SerialNumbers {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.txIDsToSerialNumbers[tx]
}

func (p *pendingCommits) doNotExist(serialNumbers [][]byte) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, sn := range serialNumbers {
		if _, ok := p.serialNumbers[string(sn)]; ok {
			return false
		}
	}

	return true
}

func (p *pendingCommits) count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.txIDsToSerialNumbers)
}

func (p *pendingCommits) delete(tx txID) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sNumbers, ok := p.txIDsToSerialNumbers[tx]
	if !ok {
		return
	}

	for _, sn := range sNumbers.SerialNumbers {
		delete(p.serialNumbers, string(sn))
	}
	delete(p.txIDsToSerialNumbers, tx)
}

func (p *pendingCommits) deleteAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for txID, sNumbers := range p.txIDsToSerialNumbers {
		for _, sn := range sNumbers.SerialNumbers {
			delete(p.serialNumbers, string(sn))
		}
		delete(p.txIDsToSerialNumbers, txID)
	}
}
