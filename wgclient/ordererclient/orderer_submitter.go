package ordererclient

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type FabricOrdererBroadcasterOpts struct {
	Endpoints            []*connection.Endpoint
	Credentials          credentials.TransportCredentials
	Parallelism          int
	InputChannelCapacity int
	OrdererType          utils.ConsensusType
	OnAck                func(error)
}

type fabricOrdererBroadcaster struct {
	sendEnvelope        func(*common.Envelope)
	closeStreamsAndWait func() error
	ordererType         utils.ConsensusType
	streamsByOrderer    ordererStreams
}

type ordererStreams [][]*fabricOrdererStream

func (os ordererStreams) Flatten() []*fabricOrdererStream {
	var res []*fabricOrdererStream

	for _, streams := range os {
		res = append(res, streams...)
	}
	return res
}

func NewFabricOrdererBroadcaster(opts *FabricOrdererBroadcasterOpts) (*fabricOrdererBroadcaster, error) {
	connections, err := openConnections(opts.Endpoints, opts.Credentials)
	if err != nil {
		return nil, err
	}

	streamsByOrderer := openStreamsByOrderer(connections, opts.Parallelism, opts.InputChannelCapacity, opts.OnAck)

	return &fabricOrdererBroadcaster{
		closeStreamsAndWait: connectionCloser(streamsByOrderer, connections),
		ordererType:         opts.OrdererType,
		streamsByOrderer:    streamsByOrderer,
	}, nil
}

func connectionCloser(streamsByOrderer ordererStreams, connections []*grpc.ClientConn) func() error {
	return func() error {
		for _, s := range streamsByOrderer.Flatten() {
			close(s.input)
			<-s.channelClosed
		}
		return closeConnections(connections)
	}
}

func (b *fabricOrdererBroadcaster) EnvelopeSender() func(*common.Envelope) {
	var sendCounter uint64

	switch b.ordererType {
	case utils.Raft:
		// Pick some orderer to send the envelope to
		streams := b.streamsByOrderer.Flatten()
		streamLength := uint64(len(streams))
		return func(envelope *common.Envelope) {
			streams[sendCounter%streamLength].input <- envelope
			sendCounter++
		}
	case utils.Bft:
		// Enqueue envelope into some stream for each orderer
		return func(envelope *common.Envelope) {
			for _, streamsForOrderer := range b.streamsByOrderer {
				streamsForOrderer[sendCounter%uint64(len(streamsForOrderer))].input <- envelope
				sendCounter++
			}
		}
	default:
		panic("Orderer type " + b.ordererType + " not defined")
	}
}
func (b *fabricOrdererBroadcaster) SendEnvelope(envelope *common.Envelope) {
	b.sendEnvelope(envelope)
}

func (b *fabricOrdererBroadcaster) CloseStreamsAndWait() error {
	return b.closeStreamsAndWait()
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

func openStreamsByOrderer(connections []*grpc.ClientConn, parallelism, capacity int, onAck func(error)) ordererStreams {
	logger.Infof("Opening %d streams using the %d connections to the orderers.\n", parallelism, len(connections))

	var submitters [][]*fabricOrdererStream

	for i := 0; i < len(connections); i++ {
		var submittersForOrderer []*fabricOrdererStream

		for j := 0; j < parallelism/len(connections); j++ {
			submittersForOrderer = append(submittersForOrderer, newFabricOrdererStream(connections[i], capacity, onAck))
		}

		submitters = append(submitters, submittersForOrderer)
	}
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
			err := s.client.Send(envelope)
			utils.Must(err)
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
