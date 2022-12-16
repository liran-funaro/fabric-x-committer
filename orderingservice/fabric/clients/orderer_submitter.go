package clients

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/msp"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients/pkg/identity"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Multi Submitter

//MultiFabricOrdererSubmitter can connect to multiple orderers. For each ordrerer it can open multiple streams and send messages.
type MultiFabricOrdererSubmitter struct {
	providers []*FabricOrdererSubmitterProvider
}
type MultiFabricOrdererSubmitterOpts struct {
	ChannelID   string
	Endpoints   []*connection.Endpoint
	Credentials credentials.TransportCredentials
	Signer      msp.SigningIdentity
}

func NewMultiFabricOrdererSubmitter(opts *MultiFabricOrdererSubmitterOpts) (*MultiFabricOrdererSubmitter, error) {
	providers := make([]*FabricOrdererSubmitterProvider, len(opts.Endpoints))
	for i, endpoint := range opts.Endpoints {
		provider, err := NewFabricOrdererSubmitterProvider(&FabricOrdererConnectionOpts{
			ChannelID:   opts.ChannelID,
			Endpoint:    *endpoint,
			Credentials: opts.Credentials,
			Signer:      opts.Signer,
		})

		if err != nil {
			fmt.Println("Error connecting:", err)
			closeErrs := closeAll(providers[:i])
			if closeErrs != nil {
				fmt.Println(closeErrs)
			}
			return nil, err
		}

		providers[i] = provider
	}
	return &MultiFabricOrdererSubmitter{providers: providers}, nil
}
func (s *MultiFabricOrdererSubmitter) Close() error {
	return closeAll(s.providers)
}

func closeAll(closers []*FabricOrdererSubmitterProvider) error {
	errs := make([]error, 0, len(closers))
	for _, closer := range closers {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Errorf("errors while closing: %v", errs)
	}
	return nil
}

//BroadcastRepeat opens parallelism routines and on each routine it sends the same message (msgData) msgsPerGo times
func (s *MultiFabricOrdererSubmitter) BroadcastRepeat(parallelism, msgsPerGo int, msgData []byte, onAck func(error)) *sync.WaitGroup {
	return s.broadcast(parallelism, onAck, func(submitter *FabricOrdererSubmitter) {
		for i := 0; i < msgsPerGo; i++ {
			// TODO pre-generate signed envelopes
			submitter.Broadcast(msgData)
		}
	})
}

//BroadcastChannel opens parallelism routines that consume from the same dataCh channel until it closes
func (s *MultiFabricOrdererSubmitter) BroadcastChannel(parallelism int, dataCh <-chan []byte, onAck func(error)) *sync.WaitGroup {
	return s.broadcast(parallelism, onAck, func(provider *FabricOrdererSubmitter) {
		for {
			if msgData, ok := <-dataCh; ok {
				provider.Broadcast(msgData)
			} else {
				return
			}
		}
	})
}

func (s *MultiFabricOrdererSubmitter) broadcast(parallelism int, onAck func(error), submit func(*FabricOrdererSubmitter)) *sync.WaitGroup {
	var wg sync.WaitGroup
	wg.Add(parallelism)
	for i := 0; i < parallelism; i++ {
		go func(provider *FabricOrdererSubmitterProvider) {
			submitter, err := provider.NewSubmitter(onAck)

			if err != nil {
				fmt.Println("Error connecting:", err)
				return
			}
			submit(submitter)
			err = submitter.StopAndWait()
			if err != nil {
				fmt.Print(err)
			}
			wg.Done()
		}(s.providers[i%len(s.providers)])
	}
	return &wg
}

// Provider

//FabricOrdererSubmitterProvider connects to one ordrerer and can open multiple streams to send messages to this orderer
type FabricOrdererSubmitterProvider struct {
	connection *grpc.ClientConn
	channelID  string
	signer     identity.SignerSerializer
}

func NewFabricOrdererSubmitterProvider(opts *FabricOrdererConnectionOpts) (*FabricOrdererSubmitterProvider, error) {
	conn, err := connect(opts.Endpoint, opts.Credentials)
	if err != nil {
		return nil, err
	}

	return &FabricOrdererSubmitterProvider{conn, opts.ChannelID, opts.Signer}, nil
}

//NewSubmitter opens a new stream (atomic broadcast client) to an orderer)
func (p *FabricOrdererSubmitterProvider) NewSubmitter(onAck func(error)) (*FabricOrdererSubmitter, error) {
	client, err := ab.NewAtomicBroadcastClient(p.connection).Broadcast(context.TODO())
	if err != nil {
		fmt.Println("Error connecting:", err)
		return nil, err
	}
	return newFabricOrdererSubmitter(client, p.channelID, p.signer, onAck), nil
}

func (p *FabricOrdererSubmitterProvider) Close() error {
	return p.connection.Close()
}

// Submitter

//FabricOrdererSubmitter holds the reference to a stream to the orderer. It can send data to this orderer.
type FabricOrdererSubmitter struct {
	client *broadcastClient

	done    chan struct{}
	stopped bool
	sent    uint64
	acked   uint64
	once    sync.Once
}

func newFabricOrdererSubmitter(client ab.AtomicBroadcast_BroadcastClient, channelID string, signer identity.SignerSerializer, onAck func(error)) *FabricOrdererSubmitter {
	s := &FabricOrdererSubmitter{client: newBroadcastClient(client, channelID, signer), done: make(chan struct{})}
	s.startAckListener(onAck)
	return s
}

func (s *FabricOrdererSubmitter) Broadcast(msgData []byte) {
	if err := s.client.broadcast(msgData); err != nil {
		panic(err)
	} else {
		atomic.AddUint64(&s.sent, 1)
	}
}

func (s *FabricOrdererSubmitter) StopAndWait() error {
	s.stopSending()
	s.waitUntilAllAcked()
	return s.closeSend()
}

func (s *FabricOrdererSubmitter) stopSending() {
	s.stopped = true
	if atomic.LoadUint64(&s.sent) == atomic.LoadUint64(&s.acked) {
		s.once.Do(func() {
			close(s.done)
		})
	}
}
func (s *FabricOrdererSubmitter) waitUntilAllAcked() {
	<-s.done
}

func (s *FabricOrdererSubmitter) closeSend() error {
	return s.client.client.CloseSend()
}
func (s *FabricOrdererSubmitter) startAckListener(onAck func(error)) {
	go func() {
		var err error

		for !s.stopped || atomic.LoadUint64(&s.sent) > s.acked {
			err = s.client.getAck()
			onAck(err)
			s.acked++
		}
		if err != nil {
			fmt.Printf("\nError: %v\n", err)
		}
		s.once.Do(func() {
			close(s.done)
		})
	}()
}

// Broadcast client

type broadcastClient struct {
	client    ab.AtomicBroadcast_BroadcastClient
	signer    identity.SignerSerializer
	channelID string
	txCnt     uint64
}

// newBroadcastClient creates a simple instance of the broadcastClient interface
func newBroadcastClient(client ab.AtomicBroadcast_BroadcastClient, channelID string, signer identity.SignerSerializer) *broadcastClient {
	return &broadcastClient{client: client, channelID: channelID, signer: signer}
}

func (s *broadcastClient) broadcast(transaction []byte) error {
	// TODO replace cb.ConfigValue with "our" transaction

	seqNo := atomic.AddUint64(&s.txCnt, 1)

	env, err := CreateEnvelope(cb.HeaderType_MESSAGE, s.channelID, s.signer, &cb.ConfigValue{Value: transaction}, 0, 0, seqNo, nil)
	if err != nil {
		panic(err)
	}
	return s.client.Send(env)
}

func (s *broadcastClient) getAck() error {
	msg, err := s.client.Recv()
	if err != nil {
		return err
	}
	if msg.Status != cb.Status_SUCCESS {
		return fmt.Errorf("got unexpected status: %v - %s", msg.Status, msg.Info)
	}
	return nil
}

func CreateSignedEnvelope(
	txType common.HeaderType,
	channelID string,
	signer identity.SignerSerializer,
	dataMsg proto.Message,
	msgVersion int32,
	epoch uint64,
	tlsCertHash []byte,
) (*common.Envelope, error) {
	payloadChannelHeader := protoutil.MakeChannelHeader(txType, msgVersion, channelID, epoch)
	payloadChannelHeader.TlsCertHash = tlsCertHash
	var err error
	payloadSignatureHeader := &common.SignatureHeader{}

	if signer != nil {
		payloadSignatureHeader, err = protoutil.NewSignatureHeader(signer)
		if err != nil {
			return nil, err
		}
	}

	data, err := proto.Marshal(dataMsg)
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling")
	}

	paylBytes := protoutil.MarshalOrPanic(
		&common.Payload{
			Header: protoutil.MakePayloadHeader(payloadChannelHeader, payloadSignatureHeader),
			Data:   data,
		},
	)

	var sig []byte
	if signer != nil {
		sig, err = signer.Sign(paylBytes)
		if err != nil {
			return nil, err
		}
	}

	env := &common.Envelope{
		Payload:   paylBytes,
		Signature: sig,
	}

	return env, nil
}

func NewSignatureHeader(id identity.Serializer) (*cb.SignatureHeader, error) {
	//creator, err := id.Serialize()
	//if err != nil {
	//	return nil, err
	//}

	creator := []byte("bob")

	nonce, err := protoutil.CreateNonce()
	if err != nil {
		return nil, err
	}

	return &cb.SignatureHeader{
		Creator: creator,
		Nonce:   nonce,
	}, nil
}

// CreateEnvelope create an envelope WITHOUT a signature and the corresponding header
// can only be used with a patched fabric orderer
func CreateEnvelope(
	txType common.HeaderType,
	channelID string,
	signer identity.SignerSerializer,
	dataMsg proto.Message,
	msgVersion int32,
	epoch uint64,
	seqno uint64,
	tlsCertHash []byte,
) (*common.Envelope, error) {
	payloadChannelHeader := protoutil.MakeChannelHeader(txType, msgVersion, channelID, epoch)
	payloadChannelHeader.TlsCertHash = tlsCertHash
	payloadChannelHeader.TxId = fmt.Sprintf("%d", seqno)
	var err error

	data, err := proto.Marshal(dataMsg)
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling")
	}

	// TODO create a "lightweight" header
	sigHeader, err := NewSignatureHeader(signer)

	paylBytes := protoutil.MarshalOrPanic(
		&common.Payload{
			Header: &cb.Header{
				ChannelHeader:   protoutil.MarshalOrPanic(payloadChannelHeader),
				SignatureHeader: protoutil.MarshalOrPanic(sigHeader),
			},
			Data: data,
		},
	)

	env := &common.Envelope{
		Payload:   paylBytes,
		Signature: nil,
	}

	return env, nil
}
