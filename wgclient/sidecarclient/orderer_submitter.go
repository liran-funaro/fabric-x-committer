package sidecarclient

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type FabricOrdererBroadcasterOpts struct {
	Endpoints            []*connection.Endpoint
	Credentials          credentials.TransportCredentials
	Parallelism          int
	InputChannelCapacity int
	OnAck                func(error)
}

type fabricOrdererBroadcaster struct {
	streams          []OrdererStream
	closeConnections func() error
}
type OrdererStream interface {
	Input() chan<- *common.Envelope
	WaitStreamClosed()
}

func NewFabricOrdererBroadcaster(opts *FabricOrdererBroadcasterOpts) (*fabricOrdererBroadcaster, error) {
	connections, err := openConnections(opts.Endpoints, opts.Credentials)
	if err != nil {
		return nil, err
	}

	return &fabricOrdererBroadcaster{
		streams:          openStreams(connections, opts.Parallelism, opts.InputChannelCapacity, opts.OnAck),
		closeConnections: func() error { return closeConnections(connections) },
	}, nil
}

func (b *fabricOrdererBroadcaster) Streams() []OrdererStream {
	return b.streams
}

func (b *fabricOrdererBroadcaster) CloseStreamsAndWait() error {
	for _, s := range b.Streams() {
		close(s.Input())
		s.WaitStreamClosed()
	}
	return b.closeConnections()
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

func openStreams(connections []*grpc.ClientConn, parallelism, capacity int, onAck func(error)) []OrdererStream {
	logger.Infof("Opening %d streams using the %d connections to the orderers.\n", parallelism, len(connections))
	submitters := make([]OrdererStream, parallelism)

	var wg sync.WaitGroup
	wg.Add(parallelism)
	for i := 0; i < parallelism; i++ {
		go func(conn *grpc.ClientConn, i int) {
			submitters[i] = newFabricOrdererStream(conn, capacity, onAck)
			wg.Done()
		}(connections[i%len(connections)], i)
	}
	wg.Wait()
	return submitters
}

type fabricOrdererStream struct {
	client        ab.AtomicBroadcast_BroadcastClient
	input         chan *common.Envelope
	allAcked      chan struct{}
	channelClosed chan struct{}

	stopped bool
	sent    uint64
	acked   uint64
	once    sync.Once
	txCnt   uint64
}

func newFabricOrdererStream(conn *grpc.ClientConn, capacity int, onAck func(error)) *fabricOrdererStream {
	client, err := ab.NewAtomicBroadcastClient(conn).Broadcast(context.TODO())
	utils.Must(err)
	s := &fabricOrdererStream{
		client:        client,
		input:         make(chan *common.Envelope, capacity),
		allAcked:      make(chan struct{}),
		channelClosed: make(chan struct{}),
	}
	s.startAckListener(onAck)
	s.startEnvelopeSender()
	return s
}

func (s *fabricOrdererStream) Input() chan<- *common.Envelope {
	return s.input
}

func (s *fabricOrdererStream) WaitStreamClosed() {
	<-s.channelClosed
}

func (s *fabricOrdererStream) startAckListener(onAck func(error)) {
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
			close(s.allAcked)
		})
	}()
}

func (s *fabricOrdererStream) getAck() error {
	msg, err := s.client.Recv()
	if err != nil {
		return err
	}
	if msg.Status != common.Status_SUCCESS {
		return fmt.Errorf("got unexpected status: %v - %s", msg.Status, msg.Info)
	}
	return nil
}
func (s *fabricOrdererStream) startEnvelopeSender() {
	go func() {
		for envelope := range s.input {
			utils.Must(s.client.Send(envelope))
			atomic.AddUint64(&s.sent, 1)
		}

		s.waitAcksAndCloseStream()

	}()
}

func (s *fabricOrdererStream) waitAcksAndCloseStream() {
	s.stopSending()
	<-s.allAcked
	if err := s.client.CloseSend(); err != nil {
		logger.Infof("Error occurred while closing the channel: %v", err)
	}
	close(s.channelClosed)
}

func (s *fabricOrdererStream) stopSending() {
	s.stopped = true
	if atomic.LoadUint64(&s.sent) == atomic.LoadUint64(&s.acked) {
		s.once.Do(func() {
			close(s.allAcked)
		})
	}
}
