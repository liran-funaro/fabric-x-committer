package pipeline

import (
	"context"
	"fmt"
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type sigVerifierMgr struct {
	verifiers []*sigVerifier

	inputChan              chan *token.Block
	responseCollectionChan chan *sigverification.ResponseBatch
	outputChanValids       chan []TxSeqNum
	outputChanInvalids     chan []TxSeqNum

	stopSignalCh chan struct{}
	metrics      *metrics.Metrics
}

func newSigVerificationMgr(c *SigVerifierMgrConfig, metrics *metrics.Metrics) (*sigVerifierMgr, error) {
	responseCollectionChan := make(chan *sigverification.ResponseBatch, defaultChannelBufferSize)

	verifiers := []*sigVerifier{}
	for _, a := range c.Endpoints {
		v, err := newSigVerifier(a, responseCollectionChan)
		if err != nil {
			return nil, err
		}
		verifiers = append(verifiers, v)
	}

	m := &sigVerifierMgr{
		verifiers:              verifiers,
		inputChan:              make(chan *token.Block, defaultChannelBufferSize),
		responseCollectionChan: responseCollectionChan,
		outputChanValids:       make(chan []TxSeqNum, defaultChannelBufferSize),
		outputChanInvalids:     make(chan []TxSeqNum, defaultChannelBufferSize),
		stopSignalCh:           make(chan struct{}),
		metrics:                metrics,
	}
	if metrics.Enabled {
		metrics.SigVerifierMgrInputChLength.SetCapacity(defaultChannelBufferSize)
		metrics.SigVerifierMgrValidOutputChLength.SetCapacity(defaultChannelBufferSize)
		metrics.SigVerifierMgrInvalidOutputChLength.SetCapacity(defaultChannelBufferSize)
	}

	m.startBlockReceiverRoutine()
	m.startOutputWriterRoutine()
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
				if m.metrics.Enabled {
					m.metrics.SigVerifierMgrInTxs.Add(len(b.Txs))
					m.metrics.SigVerifierMgrInputChLength.Set(len(m.inputChan))
				}
			}
		}
	}()
}

func (m *sigVerifierMgr) startOutputWriterRoutine() {
	go func() {
		for {
			select {
			case <-m.stopSignalCh:
				close(m.outputChanValids)
				close(m.outputChanInvalids)
				return
			case responseBatch := <-m.responseCollectionChan:
				valids := []TxSeqNum{}
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
					if m.metrics.Enabled {
						m.metrics.SigVerifierMgrInvalidOutputChLength.Set(len(m.outputChanInvalids))
					}
				}

				if len(valids) > 0 {
					m.outputChanValids <- valids
					if m.metrics.Enabled {
						m.metrics.SigVerifierMgrValidOutputChLength.Set(len(m.outputChanValids))
					}
				}
				if m.metrics.Enabled {
					m.metrics.SigVerifierMgrOutTxs.Add(len(responseBatch.Responses))
				}
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

func newSigVerifier(endpoint *connection.Endpoint, responseCollectionChan chan<- *sigverification.ResponseBatch) (*sigVerifier, error) {

	conn, err := grpc.Dial(
		endpoint.Address(),
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
		client:              client,
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
