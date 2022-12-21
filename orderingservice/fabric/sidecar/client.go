package sidecar

import (
	"fmt"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"sync"
)

type ClientInitOptions struct {
	SidecarEndpoint connection.Endpoint

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
	sidecarListener    *clients.SidecarListener
	ordererBroadcaster *clients.FabricOrdererBroadcaster
}

func NewClient(opts *ClientInitOptions) (*Client, error) {
	listener, err := clients.NewSidecarListener(opts.SidecarEndpoint)
	if err != nil {
		return nil, err
	}

	submitter, err := clients.NewFabricOrdererBroadcaster(&clients.FabricOrdererBroadcasterOpts{
		ChannelID:            opts.ChannelID,
		Endpoints:            opts.OrdererEndpoints,
		Credentials:          opts.OrdererTransportCredentials,
		Signer:               opts.OrdererSigner,
		Parallelism:          opts.Parallelism,
		InputChannelCapacity: opts.InputChannelCapacity,
		OnAck: func(err error) {

		},
	})
	if err != nil {
		return nil, err
	}

	return &Client{listener, submitter}, nil
}

//StartListening listens for incoming committed blocks from sidecar
func (c *Client) StartListening(onBlock func(*common.Block), onError func(error)) {
	c.sidecarListener.StartListening(onBlock, onError)
}

//Send sends a transaction to the orderer
func (c *Client) Send(tx *token.Tx) error {
	message, err := proto.Marshal(tx)
	if err != nil {
		return err
	}
	c.ordererBroadcaster.Send(message)
	return nil
}

func (c *Client) SendRepeated(msgSize, msgsPerGo int) *sync.WaitGroup {
	return c.ordererBroadcaster.SendRepeated(make([]byte, msgSize), msgsPerGo)
}
func (c *Client) SendChannel(ch <-chan []byte) *sync.WaitGroup {
	return c.ordererBroadcaster.SendChannel(ch)
}
func (c *Client) SendBulk(getNext func(int, int) (*token.Tx, bool)) *sync.WaitGroup {
	return c.ordererBroadcaster.SendBulk(func(i int, s int) ([]byte, bool) {
		tx, ok := getNext(i, s)
		if !ok {
			return nil, false
		}
		message, err := proto.Marshal(tx)
		if err != nil {
			fmt.Println(err)
			return nil, false
		}
		return message, true
	})
}

func (c *Client) Close() error {
	err := c.ordererBroadcaster.Close()
	return err
}
