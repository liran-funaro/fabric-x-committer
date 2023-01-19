package sidecarclient

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/msp"
	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type FabricOrdererBroadcasterOpts struct {
	ChannelID            string
	Endpoints            []*connection.Endpoint
	Credentials          credentials.TransportCredentials
	Signer               msp.SigningIdentity
	SignedEnvelopes      bool
	Parallelism          int
	InputChannelCapacity int
	OnAck                func(error)
}

type fabricOrdererBroadcaster struct {
	submitters       []*fabricOrdererSubmitter
	messageChs       []chan []byte
	wg               *sync.WaitGroup
	closeConnections func() error
}

func NewFabricOrdererBroadcaster(opts *FabricOrdererBroadcasterOpts) (*fabricOrdererBroadcaster, error) {
	connections, err := openConnections(opts.Endpoints, opts.Credentials)
	if err != nil {
		return nil, err
	}

	b := &fabricOrdererBroadcaster{
		submitters:       openStreams(connections, opts.Parallelism, opts.ChannelID, opts.Signer, opts.SignedEnvelopes, opts.OnAck),
		messageChs:       createChannels(opts.Parallelism, opts.InputChannelCapacity),
		wg:               &sync.WaitGroup{},
		closeConnections: func() error { return closeConnections(connections) },
	}

	go func() {
		b.startSending()
	}()

	return b, nil
}

func (b *fabricOrdererBroadcaster) InputChannels() []chan []byte {
	return b.messageChs
}

func (b *fabricOrdererBroadcaster) CloseAndWait() error {
	for _, ch := range b.messageChs {
		close(ch)
	}
	b.wg.Wait()
	return b.closeConnections()
}

func createChannels(length, capacity int) []chan []byte {
	messageChs := make([]chan []byte, length)
	for s := 0; s < len(messageChs); s++ {
		messageChs[s] = make(chan []byte, capacity)
	}
	return messageChs
}

func (b *fabricOrdererBroadcaster) startSending() {
	b.wg.Add(len(b.submitters))
	for i := range b.submitters {
		go func(submitter *fabricOrdererSubmitter, messages <-chan []byte) {
			for message := range messages {
				submitter.Broadcast(message)
			}
			if err := submitter.StopAndWait(); err != nil {
				fmt.Println(err)
			}
			b.wg.Done()
		}(b.submitters[i], b.messageChs[i])
	}
}

func openConnections(endpoints []*connection.Endpoint, transportCredentials credentials.TransportCredentials) ([]*grpc.ClientConn, error) {
	logger.Infof("Opening connections to %d orderers: %v.\n", len(endpoints), endpoints)
	connections := make([]*grpc.ClientConn, len(endpoints))
	for i, endpoint := range endpoints {
		conn, err := connection.Connect(connection.NewDialConfigWithCreds(*endpoint, transportCredentials))

		if err != nil {
			logger.Errorf("Error connecting: %v", err)
			closeErrs := closeConnections(connections[:i])
			if closeErrs != nil {
				logger.Error(closeErrs)
			}
			return nil, err
		}

		connections[i] = conn
	}
	return connections, nil
}

func closeConnections(connections []*grpc.ClientConn) error {
	logger.Infof("Closing %d connections.\n", len(connections))
	errs := make([]error, 0, len(connections))
	for _, closer := range connections {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Errorf("errors while closing: %v", errs)
	}
	return nil
}

func openStreams(connections []*grpc.ClientConn, parallelism int, channelID string, signer msp.SigningIdentity, signed bool, onAck func(error)) []*fabricOrdererSubmitter {
	logger.Infof("Opening %d streams for channel '%s' using the %d connections to the orderers.\n", parallelism, channelID, len(connections))
	submitters := make([]*fabricOrdererSubmitter, parallelism)

	var wg sync.WaitGroup
	wg.Add(parallelism)
	for i := 0; i < parallelism; i++ {
		go func(conn *grpc.ClientConn, i int) {
			client, err := ab.NewAtomicBroadcastClient(conn).Broadcast(context.TODO())
			if err != nil {
				fmt.Println("Error connecting:", err)
				return
			}
			submitters[i] = newFabricOrdererSubmitter(client, channelID, signer, signed, onAck)
			wg.Done()
		}(connections[i%len(connections)], i)
	}
	wg.Wait()
	return submitters
}

// Submitter

//fabricOrdererSubmitter holds the reference to a stream to the orderer. It can send data to this orderer.
type fabricOrdererSubmitter struct {
	client          ab.AtomicBroadcast_BroadcastClient
	envelopeCreator EnvelopeCreator
	done            chan struct{}

	stopped bool
	sent    uint64
	acked   uint64
	once    sync.Once
	txCnt   uint64
}

func newFabricOrdererSubmitter(client ab.AtomicBroadcast_BroadcastClient, channelID string, signer msp.SigningIdentity, signed bool, onAck func(error)) *fabricOrdererSubmitter {
	s := &fabricOrdererSubmitter{
		client:          client,
		envelopeCreator: newEnvelopeCreator(channelID, signer, signed),
		done:            make(chan struct{}),
	}
	s.startAckListener(onAck)
	return s
}

func (s *fabricOrdererSubmitter) Broadcast(msgData []byte) {
	if err := s.broadcast(msgData); err != nil {
		panic(err)
	} else {
		atomic.AddUint64(&s.sent, 1)
	}
}

func (s *fabricOrdererSubmitter) StopAndWait() error {
	s.stopSending()
	s.waitUntilAllAcked()
	return s.closeSend()
}

func (s *fabricOrdererSubmitter) stopSending() {
	s.stopped = true
	if atomic.LoadUint64(&s.sent) == atomic.LoadUint64(&s.acked) {
		s.once.Do(func() {
			close(s.done)
		})
	}
}
func (s *fabricOrdererSubmitter) waitUntilAllAcked() {
	<-s.done
}

func (s *fabricOrdererSubmitter) closeSend() error {
	return s.client.CloseSend()
}
func (s *fabricOrdererSubmitter) startAckListener(onAck func(error)) {
	go func() {
		var err error

		for !s.stopped || atomic.LoadUint64(&s.sent) > s.acked {
			err = s.getAck()
			onAck(err)
			s.acked++
		}
		if err != nil {
			logger.Errorf("\nError: %v\n", err)
		}
		s.once.Do(func() {
			close(s.done)
		})
	}()
}

func (s *fabricOrdererSubmitter) broadcast(transaction []byte) error {
	// TODO replace cb.ConfigValue with "our" transaction

	env, err := s.envelopeCreator.CreateEnvelope(transaction)
	if err != nil {
		panic(err)
	}
	return s.client.Send(env)
}

func (s *fabricOrdererSubmitter) getAck() error {
	msg, err := s.client.Recv()
	if err != nil {
		return err
	}
	if msg.Status != common.Status_SUCCESS {
		return fmt.Errorf("got unexpected status: %v - %s", msg.Status, msg.Info)
	}
	return nil
}
