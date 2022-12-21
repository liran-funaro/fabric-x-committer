package sidecar

import (
	"fmt"
	"sync"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

type ClientInitOptions struct {
	CommitterEndpoint connection.Endpoint
	SidecarEndpoint   connection.Endpoint

	OrdererSecurityOpts         *clients.SecurityConnectionOpts
	ChannelID                   string
	OrdererTransportCredentials credentials.TransportCredentials
	OrdererEndpoints            []*connection.Endpoint
	OrdererSigner               msp.SigningIdentity

	Parallelism          int
	InputChannelCapacity int
}

//Client connects to:
//1. the orderer where it sends transactions to be ordered, and
//2. the sidecar where it listens for committed blocks
type Client struct {
	committerClient    *client.CoordinatorAdapter
	sidecarListener    *clients.SidecarListener
	ordererBroadcaster *clients.FabricOrdererBroadcaster
}

func NewClient(opts *ClientInitOptions) (*Client, error) {
	logger.Infof("Connecting client to:\n"+
		"\tCommitter: %v\n"+
		"\tSidecar: %v\n"+
		"\tOrderers: %v (%s)\n", opts.CommitterEndpoint, opts.SidecarEndpoint, opts.OrdererEndpoints, opts.ChannelID)
	committer := client.OpenCoordinatorAdapter(opts.CommitterEndpoint)

	listener, err := clients.NewSidecarListener(opts.SidecarEndpoint)
	if err != nil {
		return nil, err
	}

	submitter, err := clients.NewFabricOrdererBroadcaster(&clients.FabricOrdererBroadcasterOpts{
		ChannelID:            opts.ChannelID,
		Endpoints:            opts.OrdererEndpoints,
		SecurityOpts:         opts.OrdererSecurityOpts,
		Parallelism:          opts.Parallelism,
		InputChannelCapacity: opts.InputChannelCapacity,
		OnAck:                func(err error) {},
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

func (c *Client) SendReplicated(getItem func() (*token.Tx, bool)) *sync.WaitGroup {
	return c.ordererBroadcaster.SendReplicated(func() ([]byte, bool) {
		tx, ok := getItem()
		if !ok {
			return nil, false
		}
		data, err := proto.Marshal(tx)
		if err != nil {
			fmt.Printf("Error occurred: %v\n", err)
			return nil, false
		}
		return data, true
	})
}

func (c *Client) SendRepeated(msgSize, msgsPerGo int) *sync.WaitGroup {
	return c.ordererBroadcaster.SendRepeated(make([]byte, msgSize), msgsPerGo)
}

func (c *Client) Close() error {
	err := c.ordererBroadcaster.Close()
	return err
}
