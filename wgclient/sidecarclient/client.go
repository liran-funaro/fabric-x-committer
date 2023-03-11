package sidecarclient

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/deliver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/serialization"
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
	//Streams returns all streams to the ordering service
	Streams() []OrdererStream
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
	SignedEnvelopes    bool

	Parallelism          int
	InputChannelCapacity int

	RemoteControllerListener string
}

// Client connects to:
// 1. the orderer where it sends transactions to be ordered, and
// 2. the sidecar where it listens for committed blocks
type Client struct {
	committerClient          VerificationKeySetter
	sidecarListener          sidecar.DeliverListener
	ordererBroadcaster       OrdererSubmitter
	envelopeCreator          EnvelopeCreator
	RemoteControllerListener string
}

func NewClient(opts *ClientInitOptions) (*Client, error) {
	logger.Infof("Connecting client to:\n"+
		"\tCommitter: %v\n"+
		"\tSidecar: %v\n"+
		"\tOrderers: %v (channel: %s, signed envelopes: %v)\n", &opts.CommitterEndpoint, &opts.SidecarEndpoint, opts.OrdererEndpoints, opts.ChannelID, opts.SignedEnvelopes)
	committer := client.OpenCoordinatorAdapter(opts.CommitterEndpoint)

	listener, err := deliver.NewListener(&deliver.ConnectionOpts{
		ClientProvider: &PeerDeliverClientProvider{},
		Credentials:    opts.SidecarCredentials,
		Signer:         opts.SidecarSigner,
		ChannelID:      opts.ChannelID,
		Endpoint:       opts.SidecarEndpoint,
		Reconnect:      -1,
		StartBlock:     0,
	})
	if err != nil {
		return nil, err
	}

	sent := uint64(0)
	submitter, err := NewFabricOrdererBroadcaster(&FabricOrdererBroadcasterOpts{
		Endpoints:            opts.OrdererEndpoints,
		Credentials:          opts.OrdererCredentials,
		Parallelism:          len(opts.OrdererEndpoints) * opts.Parallelism,
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

	return &Client{committer, listener, submitter, envelopeCreator, opts.RemoteControllerListener}, nil
}

func (c *Client) SetCommitterKey(publicKey signature.PublicKey) error {
	return c.committerClient.SetVerificationKey(publicKey)
}

// StartListening listens for incoming committed blocks from sidecar
func (c *Client) StartListening(onBlock func(*common.Block)) {
	utils.Must(c.sidecarListener.RunDeliverOutputListener(onBlock))
}

// SendReplicated fans out the TXs of the input channel, and then replicates and sends its result to all streams.
func (c *Client) SendReplicated(txs chan *token.Tx, onRequestSend func()) {
	logger.Infof("Sending replicated message to all orderers.\n")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			for _, messageCh := range c.ordererBroadcaster.Streams() {
				tx, ok := <-txs
				if !ok {
					return
				}
				item := serialization.MarshalTx(tx)
				env, err := c.envelopeCreator.CreateEnvelope(item)
				onRequestSend()
				utils.Must(err)
				messageCh.Input() <- env
			}
		}
	}()
	wg.Wait()
	utils.Must(c.ordererBroadcaster.CloseStreamsAndWait())
}

func (c *Client) Send(txs chan *sigverification_test.TxWithStatus, onRequestSend func(*sigverification_test.TxWithStatus, *common.Envelope)) {
	logger.Infof("Sending messages to all open streams.\n")

	var rl ratelimit.Limiter
	rl = ratelimit.NewUnlimited()

	// start remote-limiter controller
	if c.RemoteControllerListener != "" {
		fmt.Printf("Start remote controller listener on %v\n", c.RemoteControllerListener)
		gin.SetMode(gin.ReleaseMode)
		router := gin.Default()
		router.POST("/setLimits", func(c *gin.Context) {

			type Limiter struct {
				Limit int `json:"limit"`
			}

			var limit Limiter
			if err := c.BindJSON(&limit); err != nil {
				return
			}

			if limit.Limit < 1 {
				rl = ratelimit.NewUnlimited()
				return
			}

			// create our new limiter
			rl = ratelimit.New(limit.Limit)

			c.IndentedJSON(http.StatusOK, limit)
		})
		go router.Run(c.RemoteControllerListener)
	}

	var wg sync.WaitGroup
	wg.Add(len(c.ordererBroadcaster.Streams()))
	for _, ch := range c.ordererBroadcaster.Streams() {
		go func(input chan<- *common.Envelope) {
			defer wg.Done()
			for {
				select {
				case tx := <-txs:
					item := serialization.MarshalTx(tx.Tx)
					env, err := c.envelopeCreator.CreateEnvelope(item)
					utils.Must(err)
					_ = rl.Take() // our rate limiter
					input <- env
					// let's track this request once we have sent it
					onRequestSend(tx, env)
				}
			}
		}(ch.Input())
	}

	wg.Wait()
	utils.Must(c.ordererBroadcaster.CloseStreamsAndWait())
}
