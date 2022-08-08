package pipeline

import (
	"context"
	"fmt"
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"google.golang.org/grpc"
)

type sigVerifierMgr struct {
	verifiers []sigverification.Verifier_StartStreamClient

	verifierNumLock sync.Mutex
	nextVerifier    int

	resultsLock        sync.Mutex
	txValidationStatus []*txValidationStatus
}

type sigVerifierConfig struct {
	SigVerifierServers []string
}

func newSigVerificationMgr(c *sigVerifierConfig) (*sigVerifierMgr, error) {
	verifiers := []sigverification.Verifier_StartStreamClient{}
	for _, m := range c.SigVerifierServers {
		s, err := initSigVerifierStream(m)
		if err != nil {
			return nil, err
		}
		verifiers = append(verifiers, s)
	}
	return &sigVerifierMgr{
		txValidationStatus: []*txValidationStatus{},
		verifiers:          verifiers,
	}, nil
}

func (m *sigVerifierMgr) processBlockAsync(block *token.Block) {
	go func(block *token.Block) {
		batch := &sigverification.RequestBatch{
			Requests: []*sigverification.Request{},
		}
		for i, tx := range block.Txs {
			batch.Requests = append(batch.Requests, &sigverification.Request{
				BlockNum: block.Number,
				TxNum:    uint64(i),
				Tx:       tx,
			})
		}

		m.verifierNumLock.Lock()
		defer m.verifierNumLock.Unlock()
		v := m.verifiers[m.nextVerifier]
		v.Send(batch)
		m.nextVerifier++
	}(block)
}

func (m *sigVerifierMgr) addValidationResults(batch []*txValidationStatus) {
	m.resultsLock.Lock()
	m.txValidationStatus = append(m.txValidationStatus, batch...)
}

func (m *sigVerifierMgr) removeValidationResults() []*txValidationStatus {
	m.resultsLock.Lock()
	defer m.resultsLock.Unlock()
	r := m.txValidationStatus
	m.txValidationStatus = []*txValidationStatus{}
	return r
}

func initSigVerifierStream(machine string) (sigverification.Verifier_StartStreamClient, error) {
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", machine, config.GRPC_PORT), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	client := sigverification.NewVerifierClient(conn)
	stream, err := client.StartStream(context.Background())
	if err != nil {
		return nil, err
	}
	return stream, err
}
