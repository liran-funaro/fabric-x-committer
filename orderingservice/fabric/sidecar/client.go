package sidecar

import (
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

type SidecarOutputListener interface {
	RunOutputListener(onBlockReceive func(*ab.DeliverResponse_Block))
}
type TxGenerator interface {
	Next() *token.Tx
}
type OrdererSubmitter interface {
	Broadcast([]byte)
}

type Client struct {
	sidecarOutputListener SidecarOutputListener
	txGenerator           TxGenerator
	ordererSubmitter      OrdererSubmitter
}

type ClientInitOptions struct {
	ChannelID                   string
	OrdererTransportCredentials credentials.TransportCredentials
	OrdererEndpoint             connection.Endpoint
	ClientEndpoint              connection.Endpoint
}

func NewClient(opts *ClientInitOptions) (*Client, error) {
	return nil, nil
}

func (c *Client) Start() {
	c.sidecarOutputListener.RunOutputListener(func(*ab.DeliverResponse_Block) {})

	for {
		if tx, err := proto.Marshal(c.txGenerator.Next()); err == nil {
			c.ordererSubmitter.Broadcast(tx)
		}
	}
}
