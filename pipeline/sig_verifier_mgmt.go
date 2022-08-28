package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type SigVerifierMgrConfig struct {
	SigVerifierServers []string
	BatchCutConfig     *BatchCutConfig
}

type BatchCutConfig struct {
	BatchSize     int
	TimeoutMillis int
}

type sigVerifierMgr struct {
	config              *SigVerifierMgrConfig
	verifiers           []*sigVerifier
	nextVerifier        int
	verificationResults *sigVerificationResults
	stopSignalCh        chan struct{}
	stopWg              sync.WaitGroup
}

type sigVerificationResults struct {
	batchCutConfig         *BatchCutConfig
	batchCutTimeoutResetCh chan struct{}
	validsCh               chan []txSeqNum
	invalidsCh             chan []txSeqNum
	mu                     sync.Mutex
	valids                 []txSeqNum
}

func newSigVerificationMgr(c *SigVerifierMgrConfig) (*sigVerifierMgr, error) {
	verificationResults := &sigVerificationResults{
		batchCutConfig:         c.BatchCutConfig,
		batchCutTimeoutResetCh: make(chan struct{}),
		validsCh:               make(chan []txSeqNum, defaultChannelBufferSize),
		invalidsCh:             make(chan []txSeqNum, defaultChannelBufferSize),
		valids:                 []txSeqNum{},
	}

	verifiers := []*sigVerifier{}
	for _, h := range c.SigVerifierServers {
		v, err := newSigVerifier(h, verificationResults)
		if err != nil {
			return nil, err
		}
		verifiers = append(verifiers, v)
	}

	m := &sigVerifierMgr{
		config:              c,
		verifiers:           verifiers,
		verificationResults: verificationResults,
		stopSignalCh:        make(chan struct{}),
	}
	m.startTimeoutBasedBatchCutterRoutine()
	return m, nil
}

func (m *sigVerifierMgr) processBlockAsync(b *token.Block) {
	v := m.verifiers[m.nextVerifier]
	if m.nextVerifier == len(m.verifiers)-1 {
		m.nextVerifier = 0
	} else {
		m.nextVerifier++
	}
	v.sendCh <- b
}

func (r *sigVerificationResults) cutBatchWhileHoldingLock() {
	batchSize := r.batchCutConfig.BatchSize
	size := len(r.valids)
	if size == 0 {
		return
	}

	if size > batchSize {
		size = batchSize
	}

	b := r.valids[:size]
	r.valids = r.valids[size:]
	r.validsCh <- b
}

func (m *sigVerifierMgr) startTimeoutBasedBatchCutterRoutine() {
	timeoutMillis := m.config.BatchCutConfig.TimeoutMillis
	d := time.Duration(timeoutMillis * int(time.Millisecond))
	r := m.verificationResults
	go func() {
		for {
			select {
			case <-m.stopSignalCh:
				m.stopWg.Done()
				return
			case <-r.batchCutTimeoutResetCh:
			case <-time.After(d):
				func() {
					r.mu.Lock()
					defer r.mu.Unlock()
					if len(r.batchCutTimeoutResetCh) > 0 {
						// the batch already cut based on size before we aquired lock here
						return
					}
					r.cutBatchWhileHoldingLock()
				}()
			}
		}
	}()
}

func (m *sigVerifierMgr) stopTimeoutBasedBatchCutterRoutine() {
	m.stopWg.Add(1)
	m.stopSignalCh <- struct{}{}
	m.stopWg.Wait()
}

func (m *sigVerifierMgr) stop() {
	for _, v := range m.verifiers {
		v.stop()
	}
	m.stopTimeoutBasedBatchCutterRoutine()

	r := m.verificationResults
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.valids) != 0 {
		r.cutBatchWhileHoldingLock()
	}
}

///////////////////////////////////////////////////////////////////////////////////////////////

type sigVerifier struct {
	stream              sigverification.Verifier_StartStreamClient
	streamContext       context.Context
	streamContextCancel func()
	sendCh              chan *token.Block

	stopSignalCh    chan struct{}
	stoppedSignalCh chan struct{}
	stopWG          sync.WaitGroup
}

func newSigVerifier(host string, resultsDest *sigVerificationResults) (*sigVerifier, error) {
	conn, err := grpc.Dial(
		fmt.Sprintf("%s:%d", host, config.GRPC_PORT),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	client := sigverification.NewVerifierClient(conn)

	cancelableContext, cancel := context.WithCancel(context.Background())
	stream, err := client.StartStream(cancelableContext)
	if err != nil {
		return nil, err
	}

	v := &sigVerifier{
		stream:              stream,
		streamContext:       cancelableContext,
		streamContextCancel: cancel,
		sendCh:              make(chan *token.Block, defaultChannelBufferSize),
		stopSignalCh:        make(chan struct{}, 2),
		stoppedSignalCh:     make(chan struct{}, 2),
	}

	v.startRequestSenderRoutine()
	v.startResponseRecieverRoutine(resultsDest)
	return v, nil
}

func (v *sigVerifier) startRequestSenderRoutine() {
	go func() {
		for {
			select {
			case <-v.stopSignalCh:
				err := v.stream.CloseSend()
				if err != nil {
					panic(fmt.Sprintf("Error while closing the stream for sending: %s", err))
				}
				v.stopWG.Done()
				return
			case b := <-v.sendCh:
				reqs := make([]*sigverification.Request, len(b.Txs))
				for i, tx := range b.Txs {
					reqs[i] = &sigverification.Request{
						BlockNum: b.Number,
						TxNum:    uint64(i),
						Tx:       tx,
					}
				}

				err := v.stream.Send(&sigverification.RequestBatch{Requests: reqs})
				if err != nil {
					panic(fmt.Sprintf("Error while sending sig verification request batch on stream: %s", err))
				}
			}
		}
	}()
}

func (v *sigVerifier) startResponseRecieverRoutine(r *sigVerificationResults) {
	go func() {
		for {
			responseBatch, err := v.stream.Recv()
			if v.streamContext.Err() == context.Canceled {
				v.stopWG.Done()
				return
			}

			if err != nil {
				panic(fmt.Sprintf("Error while reaading sig verification response batch on stream: %s", err))
			}

			responses := responseBatch.Responses
			valids := []txSeqNum{}
			invalids := []txSeqNum{}

			for _, res := range responses {
				n := txSeqNum{
					blkNum: res.BlockNum,
					txNum:  res.TxNum,
				}
				if res.IsValid {
					valids = append(valids, n)
				} else {
					invalids = append(invalids, n)
				}
			}

			r.invalidsCh <- invalids

			func() {
				batchSize := r.batchCutConfig.BatchSize
				r.mu.Lock()
				defer r.mu.Unlock()
				r.valids = append(r.valids, valids...)
				if len(r.valids) >= batchSize {
					r.cutBatchWhileHoldingLock()
					r.batchCutTimeoutResetCh <- struct{}{}
				}
			}()
		}
	}()
}

func (v *sigVerifier) stopRequestSenderRoutine() {
	v.stopWG.Add(1)
	v.stopSignalCh <- struct{}{}
	v.stopWG.Wait()
}

func (v *sigVerifier) stopResponseRecieverRoutine() {
	v.stopWG.Add(1)
	v.streamContextCancel()
	v.stopWG.Wait()
}

func (v *sigVerifier) stop() {
	v.stopRequestSenderRoutine()
	v.stopResponseRecieverRoutine()
}
