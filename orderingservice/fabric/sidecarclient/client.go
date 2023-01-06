package sidecarclient

import (
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

var logger = logging.New("sidecarclient")

//VerificationKeySetter connects to the committer (coordinator) and sets the PK that has to be used to verify the signatures of the TXs.
type VerificationKeySetter interface {
	SetVerificationKey(signature.PublicKey) error
}

//SidecarListener connects to the sidecar and listens for committed blocks
type SidecarListener interface {
	StartListening(onBlockReceived func(*common.Block), onError func(error))
}

//OrdererSubmitter connects to the orderers, establishes one or more streams with each, and broadcasts serialized envelopes to all streams.
type OrdererSubmitter interface {
	//InputChannels returns one channel per stream.
	InputChannels() []chan []byte
	//CloseAndWait closes all input streams and waits until all envelopes have been broadcast.
	CloseAndWait() error
}

type ClientInitOptions struct {
	CommitterEndpoint connection.Endpoint
	SidecarEndpoint   connection.Endpoint

	Credentials      credentials.TransportCredentials
	Signer           msp.SigningIdentity
	ChannelID        string
	SignedEnvelopes  bool
	OrdererEndpoints []*connection.Endpoint

	Parallelism          int
	InputChannelCapacity int
}

//Client connects to:
//1. the orderer where it sends transactions to be ordered, and
//2. the sidecar where it listens for committed blocks
type Client struct {
	committerClient    VerificationKeySetter
	sidecarListener    SidecarListener
	ordererBroadcaster OrdererSubmitter
}

func NewClient(opts *ClientInitOptions) (*Client, error) {
	logger.Infof("Connecting client to:\n"+
		"\tCommitter: %v\n"+
		"\tSidecar: %v\n"+
		"\tOrderers: %v (channel: %s, signed envelopes: %v)\n", &opts.CommitterEndpoint, &opts.SidecarEndpoint, opts.OrdererEndpoints, opts.ChannelID, opts.SignedEnvelopes)
	committer := client.OpenCoordinatorAdapter(opts.CommitterEndpoint)

	listener, err := newSidecarListener(opts.SidecarEndpoint)
	if err != nil {
		return nil, err
	}

	sent := uint64(0)
	submitter, err := sidecar.NewFabricOrdererBroadcaster(&sidecar.FabricOrdererBroadcasterOpts{
		ChannelID:            opts.ChannelID,
		Endpoints:            opts.OrdererEndpoints,
		Credentials:          opts.Credentials,
		Signer:               opts.Signer,
		Parallelism:          len(opts.OrdererEndpoints),
		SignedEnvelopes:      opts.SignedEnvelopes,
		InputChannelCapacity: opts.InputChannelCapacity,
		OnAck: func(err error) {
			atomic.AddUint64(&sent, 1)
			if sent%1_000_000 == 0 {
				logger.Debugf("Sent %d M TXs so far", sent/1_000_000)
			}
		},
	})
	if err != nil {
		return nil, err
	}

	return &Client{committer, listener, submitter}, nil
}

func (c *Client) SetCommitterKey(publicKey signature.PublicKey) error {
	return c.committerClient.SetVerificationKey(publicKey)
}

//StartListening listens for incoming committed blocks from sidecar
func (c *Client) StartListening(onBlock func(*common.Block), onError func(error)) {
	c.sidecarListener.StartListening(onBlock, onError)
}

//SendReplicated fans out the TXs of the input channel, and then replicates and sends its result to all submitters.
func (c *Client) SendReplicated(txs chan *token.Tx, onRequestSend func()) {
	logger.Infof("Sending replicated message to all orderers.\n")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for tx := range txs {
			item, err := proto.Marshal(tx)
			if err != nil {
				logger.Infof("Error occurred: %v\n", err)
				break
			}
			onRequestSend()
			for _, messageCh := range c.ordererBroadcaster.InputChannels() {
				messageCh <- item
			}
		}
		wg.Done()
	}()
	wg.Wait()
	utils.Must(c.ordererBroadcaster.CloseAndWait())
}
