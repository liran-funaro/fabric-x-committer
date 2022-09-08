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

type sigVerifierMgr struct {
	config       *config.SigVerifierMgrConfig
	verifiers    []*sigVerifier
	nextVerifier int

	responseCollectionChan chan *sigverification.ResponseBatch
	outputChanValids       chan []txSeqNum
	outputChanInvalids     chan []txSeqNum

	stopSignalCh chan struct{}
	stopWg       sync.WaitGroup
}

func newSigVerificationMgr(c *config.SigVerifierMgrConfig) (*sigVerifierMgr, error) {
	responseCollectionChan := make(chan *sigverification.ResponseBatch, defaultChannelBufferSize)

	verifiers := []*sigVerifier{}
	for _, h := range c.SigVerifierServers {
		v, err := newSigVerifier(h, responseCollectionChan)
		if err != nil {
			return nil, err
		}
		verifiers = append(verifiers, v)
	}

	m := &sigVerifierMgr{
		config:                 c,
		verifiers:              verifiers,
		responseCollectionChan: responseCollectionChan,
		outputChanValids:       make(chan []txSeqNum, defaultChannelBufferSize),
		outputChanInvalids:     make(chan []txSeqNum, defaultChannelBufferSize),
		stopSignalCh:           make(chan struct{}),
	}
	m.startOutputBatchCutterRoutine()
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

func (m *sigVerifierMgr) startOutputBatchCutterRoutine() {
	timeoutMillis := time.Duration(m.config.BatchCutConfig.TimeoutMillis * int(time.Millisecond))
	batchSize := m.config.BatchCutConfig.BatchSize
	responsesChan := m.responseCollectionChan
	valids := []txSeqNum{}

	cutBatchOfValids := func() {
		size := len(valids)
		if size == 0 {
			return
		}
		if size > batchSize {
			size = batchSize
		}
		b := valids[:size]
		valids = valids[size:]
		m.outputChanValids <- b
	}

	go func() {
		for {
			select {
			case <-m.stopSignalCh:
				for len(valids) > 0 {
					cutBatchOfValids()
				}
				m.stopWg.Done()
				return
			case responseBatch := <-responsesChan:
				invalids := []txSeqNum{}
				for _, res := range responseBatch.Responses {
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
				m.outputChanInvalids <- invalids
				if len(valids) >= batchSize {
					cutBatchOfValids()
				}
			case <-time.After(timeoutMillis):
				cutBatchOfValids()
			}
		}
	}()
}

func (m *sigVerifierMgr) stopOutputBatchCutterRoutine() {
	m.stopWg.Add(1)
	m.stopSignalCh <- struct{}{}
	m.stopWg.Wait()
}

func (m *sigVerifierMgr) stop() {
	for _, v := range m.verifiers {
		v.stop()
	}
	m.stopOutputBatchCutterRoutine()
}

///////////////////////////////////////////////////////////////////////////////////////////////

type sigVerifier struct {
	stream              sigverification.Verifier_StartStreamClient
	streamContext       context.Context
	streamContextCancel func()
	sendCh              chan *token.Block

	stopSignalCh chan struct{}
	stopWG       sync.WaitGroup
}

func newSigVerifier(host string, responseCollectionChan chan<- *sigverification.ResponseBatch) (*sigVerifier, error) {
	conn, err := grpc.Dial(
		fmt.Sprintf("%s:%d", host, config.DefaultGRPCPortSigVerifier),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	client := sigverification.NewVerifierClient(conn)

	cancelableContext, cancel := context.WithCancel(context.Background())
	stream, err := client.StartStream(cancelableContext)
	if err != nil {
		cancel()
		return nil, err
	}

	v := &sigVerifier{
		stream:              stream,
		streamContext:       cancelableContext,
		streamContextCancel: cancel,
		sendCh:              make(chan *token.Block, defaultChannelBufferSize),
		stopSignalCh:        make(chan struct{}),
	}

	v.startRequestSenderRoutine()
	v.startResponseRecieverRoutine(responseCollectionChan)
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

func (v *sigVerifier) startResponseRecieverRoutine(c chan<- *sigverification.ResponseBatch) {
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
			c <- responseBatch
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
