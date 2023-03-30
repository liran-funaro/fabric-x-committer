package sidecarclient

import (
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/deliver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/limiter"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
	"go.uber.org/ratelimit"
	"google.golang.org/grpc/credentials"
)

var logger = logging.New("sidecarclient")

//VerificationKeySetter connects to the committer (coordinator) and sets the PK that has to be used to verify the signatures of the TXs.
type VerificationKeySetter interface {
	SetVerificationKey(signature.PublicKey) error
}

//OrdererSubmitter connects to the orderers, establishes one or more streams with each, and broadcasts serialized envelopes to all streams.
type OrdererSubmitter interface {
	EnvelopeSender() func(*common.Envelope)

	//CloseStreamsAndWait waits for all acks to be received and closes all streams
	CloseStreamsAndWait() error
}

type ClientInitOptions struct {
	CommitterEndpoint connection.Endpoint

	SidecarEndpoint    connection.Endpoint
	SidecarCredentials credentials.TransportCredentials
	SidecarSigner      msp.SigningIdentity

	OrdererEndpoints   []*connection.Endpoint
	OrdererCredentials credentials.TransportCredentials
	OrdererSigner      msp.SigningIdentity
	ChannelID          string
	OrdererType        utils.ConsensusType
	SignedEnvelopes    bool
	StartBlock         int64

	Parallelism          int
	InputChannelCapacity int

	RemoteControllerListener connection.Endpoint
}

// Client connects to:
// 1. the orderer where it sends transactions to be ordered, and
// 2. the sidecar where it listens for committed blocks
type Client struct {
	committerClient    VerificationKeySetter
	sidecarListener    sidecar.DeliverListener
	ordererBroadcaster OrdererSubmitter
	envelopeCreator    EnvelopeCreator
	rateLimiter        ratelimit.Limiter
	totalStreams       int
}

func NewClient(opts *ClientInitOptions) (*Client, error) {
	logger.Infof("Connecting client to:\n"+
		"\tCommitter: %v\n"+
		"\tSidecar: %v\n"+
		"\tOrderers: %v (channel: %s, signed envelopes: %v)\n", &opts.CommitterEndpoint, &opts.SidecarEndpoint, opts.OrdererEndpoints, opts.ChannelID, opts.SignedEnvelopes)

	if opts.OrdererType == utils.Bft && !areEndpointsUnique(opts.OrdererEndpoints) {
		return nil, errors.New("bft only supports one connection per orderer")
	}

	rateLimiter := limiter.New(&opts.RemoteControllerListener)
	committer := client.OpenCoordinatorAdapter(opts.CommitterEndpoint)

	listener, err := deliver.NewListener(&deliver.ConnectionOpts{
		ClientProvider: &PeerDeliverClientProvider{},
		Credentials:    opts.SidecarCredentials,
		Signer:         opts.SidecarSigner,
		ChannelID:      opts.ChannelID,
		Endpoint:       opts.SidecarEndpoint,
		Reconnect:      -1,
		StartBlock:     opts.StartBlock,
	})
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
	if err != nil {
		return nil, err
	}

	return &Client{committer, listener, submitter, envelopeCreator, rateLimiter, totalStreams}, nil
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

func (c *Client) SetCommitterKey(publicKey signature.PublicKey) error {
	return c.committerClient.SetVerificationKey(publicKey)
}

// StartListening listens for incoming committed blocks from sidecar
func (c *Client) StartListening(onBlock func(*common.Block)) {
	utils.Must(c.sidecarListener.RunDeliverOutputListener(onBlock))
}

func (c *Client) Send(txs chan *sigverification_test.TxWithStatus, onRequestSend func(*sigverification_test.TxWithStatus, *common.Envelope)) {
	logger.Infof("Sending messages to all open streams.\n")

	var wg sync.WaitGroup
	wg.Add(c.totalStreams)
	for workerID := 0; workerID < c.totalStreams; workerID++ {
		go func() {
			defer wg.Done()
			send := c.ordererBroadcaster.EnvelopeSender()
			for {
				select {
				case tx := <-txs:
					item := serialization.MarshalTx(tx.Tx)
					env, err := c.envelopeCreator.CreateEnvelope(item)
					utils.Must(err)
					_ = c.rateLimiter.Take()
					send(env)
					// let's track this request once we have sent it
					onRequestSend(tx, env)
				}
			}
		}()
	}

	wg.Wait()
	utils.Must(c.ordererBroadcaster.CloseStreamsAndWait())
}
