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
	config    *SigVerifierMgrConfig
	verifiers []*sigVerifier

	inputChan              chan *token.Block
	responseCollectionChan chan *sigverification.ResponseBatch
	outputChanValids       chan []TxSeqNum
	outputChanInvalids     chan []TxSeqNum

	stopSignalCh chan struct{}
}

func newSigVerificationMgr(c *SigVerifierMgrConfig) (*sigVerifierMgr, error) {
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
		inputChan:              make(chan *token.Block, defaultChannelBufferSize),
		responseCollectionChan: responseCollectionChan,
		outputChanValids:       make(chan []TxSeqNum, defaultChannelBufferSize),
		outputChanInvalids:     make(chan []TxSeqNum, defaultChannelBufferSize),
		stopSignalCh:           make(chan struct{}),
	}
	m.startBlockReceiverRoutine()
	m.startOutputBatchCutterRoutine()
	return m, nil
}

func (m *sigVerifierMgr) setVerificationKey(k *sigverification.Key) error {
	for _, v := range m.verifiers {
		_, err := v.client.SetVerificationKey(context.Background(), k)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *sigVerifierMgr) startBlockReceiverRoutine() {
	go func() {
		i := 0
		for {
			v := m.verifiers[i]
			if i == len(m.verifiers)-1 {
				i = 0
			} else {
				i++
			}
			select {
			case <-m.stopSignalCh:
				return
			case b := <-m.inputChan:
				v.sendCh <- b
			}
		}
	}()
}

func (m *sigVerifierMgr) startOutputBatchCutterRoutine() {
	timeoutMillis := time.Duration(m.config.BatchCutConfig.TimeoutMillis * int(time.Millisecond))
	batchSize := m.config.BatchCutConfig.BatchSize
	responsesChan := m.responseCollectionChan
	valids := []TxSeqNum{}

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
				close(m.outputChanValids)
				close(m.outputChanInvalids)
				return
			case responseBatch := <-responsesChan:
				invalids := []TxSeqNum{}
				for _, res := range responseBatch.Responses {
					n := TxSeqNum{
						BlkNum: res.BlockNum,
						TxNum:  res.TxNum,
					}
					if res.IsValid {
						valids = append(valids, n)
					} else {
						invalids = append(invalids, n)
					}
				}

				if len(invalids) > 0 {
					m.outputChanInvalids <- invalids
				}

				if len(valids) >= batchSize {
					cutBatchOfValids()
				}
			case <-time.After(timeoutMillis):
				cutBatchOfValids()
			}
		}
	}()
}

func (m *sigVerifierMgr) stop() {
	for _, v := range m.verifiers {
		v.stop()
	}
	close(m.stopSignalCh)
}

///////////////////////////////////////////////////////////////////////////////////////////////

type sigVerifier struct {
	client              sigverification.VerifierClient
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
	close(v.stopSignalCh)
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
