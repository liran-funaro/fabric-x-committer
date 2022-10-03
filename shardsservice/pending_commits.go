package shardsservice

import (
	sync "sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type pendingCommits struct {
	txIDsToSerialNumbers map[txID]*SerialNumbers
	serialNumbers        map[string][]chan struct{}
	mu                   sync.RWMutex
	logging              *logging.Logger
}

func newPendingCommits() *pendingCommits {
	return &pendingCommits{
		txIDsToSerialNumbers: make(map[txID]*SerialNumbers),
		serialNumbers:        make(map[string][]chan struct{}),
		mu:                   sync.RWMutex{},
		logging:              logging.New("pending commits"),
	}
}

func (p *pendingCommits) add(tx txID, sNumbers *SerialNumbers) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.txIDsToSerialNumbers[tx] = sNumbers
	for _, sn := range sNumbers.SerialNumbers {
		p.serialNumbers[string(sn)] = []chan struct{}{}
	}
}

func (p *pendingCommits) get(tx txID) *SerialNumbers {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.txIDsToSerialNumbers[tx]
}

func (p *pendingCommits) waitTillNotExist(serialNumbers [][]byte) {
	for _, sn := range serialNumbers {
		p.mu.RLock()
		_, ok := p.serialNumbers[string(sn)]
		p.mu.RUnlock()
		if !ok {
			continue
		}

		p.mu.Lock()
		waitingChan, ok := p.serialNumbers[string(sn)]
		if !ok {
			p.mu.Unlock()
			continue
		}

		waitOn := make(chan struct{})
		p.logging.Debug("waiting for serial number [" + string(sn) + "]")
		waitingChan = append(waitingChan, waitOn)
		p.serialNumbers[string(sn)] = waitingChan
		p.mu.Unlock()
		<-waitOn
	}
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
		waitingChan, ok := p.serialNumbers[string(sn)]
		if !ok {
			continue
		}
		for _, c := range waitingChan {
			p.logging.Debug("releasing serial number [" + string(sn) + "]")
			c <- struct{}{}
		}
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
