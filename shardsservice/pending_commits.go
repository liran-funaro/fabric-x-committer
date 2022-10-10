package shardsservice

import (
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/pendingcommits"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type pendingCommits struct {
	txIDsToSerialNumbers map[pendingcommits.TxID][]token.SerialNumber
	serialNumbers        map[string][]chan struct{}
	mu                   sync.RWMutex
	logging              *logging.Logger
}

func NewMutexPendingCommits() pendingcommits.PendingCommits {
	return &pendingCommits{
		txIDsToSerialNumbers: make(map[pendingcommits.TxID][]token.SerialNumber),
		serialNumbers:        make(map[string][]chan struct{}),
		mu:                   sync.RWMutex{},
		logging:              logging.New("pending commits"),
	}
}

func (p *pendingCommits) Add(tx pendingcommits.TxID, sNumbers []token.SerialNumber) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.txIDsToSerialNumbers[tx] = sNumbers
	for _, sn := range sNumbers {
		p.serialNumbers[string(sn)] = []chan struct{}{}
	}
}

func (p *pendingCommits) Get(tx pendingcommits.TxID) []token.SerialNumber {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.txIDsToSerialNumbers[tx]
}

func (p *pendingCommits) WaitTillNotExist(serialNumbers [][]byte) {
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

func (p *pendingCommits) CountTxs() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.txIDsToSerialNumbers)
}
func (p *pendingCommits) CountSNs() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.serialNumbers)
}

func (p *pendingCommits) Delete(tx pendingcommits.TxID) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sNumbers, ok := p.txIDsToSerialNumbers[tx]
	if !ok {
		return
	}

	for _, sn := range sNumbers {
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

func (p *pendingCommits) DeleteBatch(txIds []pendingcommits.TxID) {
	for _, tx := range txIds {
		p.Delete(tx)
	}
}

func (p *pendingCommits) DeleteAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for txID, sNumbers := range p.txIDsToSerialNumbers {
		for _, sn := range sNumbers {
			delete(p.serialNumbers, string(sn))
		}
		delete(p.txIDsToSerialNumbers, txID)
	}
}
