package ordererclient

import (
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/deliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/limiter"
	"go.uber.org/ratelimit"
	"google.golang.org/grpc/credentials"
)

var logger = logging.New("ordererclient")

// OrdererSubmitter connects to the orderers, establishes one or more streams with each, and broadcasts serialized envelopes to all streams.
type OrdererSubmitter interface {
	EnvelopeSender() func(*common.Envelope)

	//CloseStreamsAndWait waits for all acks to be received and closes all streams
	CloseStreamsAndWait() error
}

type OrdererListener interface {
	RunDeliverOutputListener(onReceive func(*common.Block)) error
}

type ClientInitOptions struct {
	DeliverClientProvider deliver.ClientProvider
	DeliverEndpoint       *connection.Endpoint
	DeliverCredentials    credentials.TransportCredentials
	DeliverSigner         msp.SigningIdentity
	StartBlock            int64

	OrdererEndpoints         []*connection.Endpoint
	OrdererCredentials       credentials.TransportCredentials
	OrdererType              utils.ConsensusType
	OrdererSigner            msp.SigningIdentity
	SignedEnvelopes          bool
	Parallelism              int
	InputChannelCapacity     int
	RemoteControllerListener *limiter.Config

	ChannelID string
}

// Client connects to:
// 1. the orderer where it sends transactions to be ordered, and
// 2. the delivery service where it listens for committed blocks
type Client struct {
	listener        OrdererListener
	submitter       OrdererSubmitter
	rateLimiter     ratelimit.Limiter
	envelopeCreator EnvelopeCreator
	totalStreams    int
}

func NewClient(opts *ClientInitOptions) (*Client, error) {
	logger.Infof("Connecting client to:\n"+
		"\tDelivery service: %v\n"+
		"\tOrderers: %v (channel: %s)\n", opts.DeliverEndpoint, opts.OrdererEndpoints, opts.ChannelID)

	if opts.OrdererType == utils.Bft && !areEndpointsUnique(opts.OrdererEndpoints) {
		return nil, errors.New("bft only supports one connection per orderer")
	}

	rateLimiter := limiter.New(opts.RemoteControllerListener)

	var listener OrdererListener
	var err error
	if opts.DeliverEndpoint != nil {
		listener, err = deliver.NewListener(&deliver.ConnectionOpts{
			ClientProvider: opts.DeliverClientProvider,
			Credentials:    opts.DeliverCredentials,
			Signer:         opts.DeliverSigner,
			ChannelID:      opts.ChannelID,
			Endpoint:       *opts.DeliverEndpoint,
			Reconnect:      -1,
			StartBlock:     opts.StartBlock,
		})
	} else {
		listener = deliver.NoopListener
	}

	if err != nil {
		return nil, err
	}

	sent := uint64(0)
	totalStreams := len(opts.OrdererEndpoints) * opts.Parallelism
	submitter, err := NewFabricOrdererBroadcaster(&FabricOrdererBroadcasterOpts{
		Endpoints:            opts.OrdererEndpoints,
		Credentials:          opts.OrdererCredentials,
		Parallelism:          totalStreams,
		OrdererType:          opts.OrdererType,
		InputChannelCapacity: opts.InputChannelCapacity,
		OnAck: func(err error) {
			atomic.AddUint64(&sent, 1)
			if sent%1_000_000 == 0 {
				logger.Debugf("Sent %d M TXs so far", sent/1_000_000)
			}
		},
	})

	envelopeCreator := NewEnvelopeCreator(opts.ChannelID, opts.OrdererSigner, opts.SignedEnvelopes)

	return &Client{listener, submitter, rateLimiter, envelopeCreator, totalStreams}, nil
}

func areEndpointsUnique(endpoints []*connection.Endpoint) bool {
	uniqueEndpoints := make(map[connection.Endpoint]bool, len(endpoints))
	for _, endpoint := range endpoints {
		if uniqueEndpoints[*endpoint] {
			return false
		}
		uniqueEndpoints[*endpoint] = true
	}
	return true
}

func (c *Client) Start(messages <-chan []byte, onSend func(EnvelopeTxId), onReceive func(*common.Block)) {
	go func() {
		utils.Must(c.listener.RunDeliverOutputListener(onReceive))
	}()

	c.send(messages, onSend)
}

func (c *Client) send(messages <-chan []byte, onRequestSend func(EnvelopeTxId)) {
	logger.Infof("Sending messages to all open streams.\n")

	var wg sync.WaitGroup
	wg.Add(c.totalStreams)
	for workerID := 0; workerID < c.totalStreams; workerID++ {
		go func() {
			defer wg.Done()
			send := c.submitter.EnvelopeSender()
			for {
				select {
				case message := <-messages:
					env, txId, _ := c.envelopeCreator.CreateEnvelope(message)
					_ = c.rateLimiter.Take()
					send(env)
					onRequestSend(txId)
				}
			}
		}()
	}

	wg.Wait()
	utils.Must(c.submitter.CloseStreamsAndWait())
}
