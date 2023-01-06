package clients

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

func NewFabricOrdererBroadcaster(opts *FabricOrdererBroadcasterOpts) (*FabricOrdererBroadcaster, error) {
	connections, err := openConnections(opts.Endpoints, opts.Credentials)
	if err != nil {
		return nil, err
	}

	submitters := openStreams(connections, opts.Parallelism, opts.ChannelID, opts.Signer, opts.SignedEnvelopes, opts.OnAck)
	closer := func() error { return closeConnections(connections) }
	return &FabricOrdererBroadcaster{submitters, closer}, nil
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

type FabricOrdererBroadcaster struct {
	submitters []*fabricOrdererSubmitter
	closer     func() error
}

//send launches a goroutine for each stream and sends the result of the i-th invocation of getItem to the s-th submitter.
//If we want to send the same message to all submitters, then for a specific value of i, getItem should return the same value (independent of s).
func (b *FabricOrdererBroadcaster) send(getItem func(int, int) ([]byte, bool)) *sync.WaitGroup {
	var wg sync.WaitGroup
	wg.Add(len(b.submitters))
	for s, submitter := range b.submitters {
		go func(submitter *fabricOrdererSubmitter, s int) {
			for m := 0; ; m++ {
				if message, ok := getItem(m, s); ok {
					submitter.Broadcast(message)
				} else {
					break
				}
			}
			if err := submitter.StopAndWait(); err != nil {
				fmt.Println(err)
			}
			wg.Done()
		}(submitter, s)
	}
	return &wg
}

//SendReplicated calls getItem, and then replicates and sends its result to all submitters.
func (b *FabricOrdererBroadcaster) SendReplicated(getItem func() ([]byte, bool)) *sync.WaitGroup {
	logger.Infof("Sending replicated message to all orderers.\n")
	chs := make([]chan []byte, len(b.submitters))
	for s := 0; s < len(chs); s++ {
		chs[s] = make(chan []byte, 10)
	}
	go func() {
		for {
			item, ok := getItem()
			if !ok {
				break
			}
			for s := 0; s < len(chs); s++ {
				chs[s] <- item
			}
		}
		for s := 0; s < len(chs); s++ {
			close(chs[s])
		}
	}()
	return b.send(func(_, s int) ([]byte, bool) {
		message, ok := <-chs[s]
		return message, ok
	})
}

//SendRepeated sends the same message (times) to all submitters times.
func (b *FabricOrdererBroadcaster) SendRepeated(message []byte, times int) *sync.WaitGroup {
	logger.Infof("Sending the same message to all servers.\n")
	return b.send(func(m, _ int) ([]byte, bool) {
		return message, m < times
	})
}

func (b *FabricOrdererBroadcaster) Close() error {
	return b.closer()
}

// Submitter

//fabricOrdererSubmitter holds the reference to a stream to the orderer. It can send data to this orderer.
type fabricOrdererSubmitter struct {
	client          BroadcastClient
	envelopeCreator *envelopeCreator
	done            chan struct{}

	stopped bool
	sent    uint64
	acked   uint64
	once    sync.Once
	txCnt   uint64
}

func newFabricOrdererSubmitter(client BroadcastClient, channelID string, signer msp.SigningIdentity, signed bool, onAck func(error)) *fabricOrdererSubmitter {
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

	seqNo := atomic.AddUint64(&s.txCnt, 1)

	env, err := s.envelopeCreator.CreateEnvelope(transaction, seqNo)
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
