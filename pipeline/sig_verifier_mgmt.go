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

type sigVerifierMgrConfig struct {
	SigVerifierServers       []string
	ValidsBatchSize          int
	ValidsBatchTimeoutMillis int
}

type sigVerifierMgr struct {
	config              *sigVerifierMgrConfig
	verifiers           []*sigVerifier
	nextVerifier        int
	verificationResults *sigVerificationResults
}

type sigVerifier struct {
	stream sigverification.Verifier_StartStreamClient
	sendCh chan *token.Block
}

type sigVerificationResults struct {
	validsBatchSize          int
	ValidsBatchTimeoutMillis int
	batchAvailableSignal     chan struct{}
	validsCh                 chan []txSeqNum
	invalidsCh               chan []txSeqNum

	mu     sync.Mutex
	valids []txSeqNum
}

func newSigVerificationMgr(c *sigVerifierMgrConfig) (*sigVerifierMgr, error) {
	verificationResults := &sigVerificationResults{
		validsBatchSize:      c.ValidsBatchSize,
		batchAvailableSignal: make(chan struct{}, defaultChannelBufferSize),
		validsCh:             make(chan []txSeqNum, defaultChannelBufferSize),
		invalidsCh:           make(chan []txSeqNum, defaultChannelBufferSize),
		valids:               []txSeqNum{},
	}
	verificationResults.startTimeoutBasedBatchCutterRoutine()

	verifiers := []*sigVerifier{}
	for _, m := range c.SigVerifierServers {
		s, err := initSigVerifierStream(m)
		if err != nil {
			return nil, err
		}
		v := &sigVerifier{
			stream: s,
			sendCh: make(chan *token.Block, defaultChannelBufferSize),
		}
		v.startRequestSenderRoutine()
		v.startResponseRecieverRoutine(verificationResults)
		verifiers = append(verifiers, v)
	}

	return &sigVerifierMgr{
		config:              c,
		verifiers:           verifiers,
		verificationResults: verificationResults,
	}, nil
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

func (v *sigVerifier) startRequestSenderRoutine() {
	go func() {
		for {
			b := <-v.sendCh
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
				panic("Error while sending sig verification request batch on stream")
			}
		}
	}()
}

func (v *sigVerifier) startResponseRecieverRoutine(r *sigVerificationResults) {
	for {
		responseBatch, err := v.stream.Recv()
		if err != nil {
			panic("Error while reaading sig verification response batch on stream")
		}

		responses := responseBatch.Responses
		valids := []txSeqNum{}
		invalids := []txSeqNum{}

		for _, r := range responses {
			n := txSeqNum{
				blkNum: r.BlockNum,
				txNum:  r.TxNum,
			}
			if r.IsValid {
				valids = append(valids, n)
			} else {
				invalids = append(invalids, n)
			}
		}

		r.invalidsCh <- invalids

		func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.valids = append(r.valids, valids...)
			// cut batch based on size
			if len(r.valids) >= r.validsBatchSize {
				b := r.valids[:r.validsBatchSize]
				r.valids = r.valids[r.validsBatchSize:]
				r.validsCh <- b
			}
		}()
	}
}

func (r *sigVerificationResults) startTimeoutBasedBatchCutterRoutine() {
	go func() {
		for {
			select {
			case <-r.batchAvailableSignal:
			case <-time.After(time.Duration(r.ValidsBatchTimeoutMillis * int(time.Millisecond))):
				func() {
					r.mu.Lock()
					defer r.mu.Unlock()
					size := len(r.valids)

					if size == 0 {
						return
					}

					if size > r.validsBatchSize {
						size = r.validsBatchSize
					}

					b := r.valids[:size]
					r.valids = r.valids[size:]
					r.validsCh <- b
				}()
			}
		}
	}()
}

func initSigVerifierStream(machine string) (sigverification.Verifier_StartStreamClient, error) {
	conn, err := grpc.Dial(
		fmt.Sprintf("%s:%d", machine, config.GRPC_PORT),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
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
