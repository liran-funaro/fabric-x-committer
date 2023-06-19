package main

import (
	"context"

	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/deliver"
	"google.golang.org/grpc"
)

type PeerDeliverClientProvider struct{}

func (p *PeerDeliverClientProvider) DeliverClient(conn *grpc.ClientConn) (deliver.Client, error) {
	client, err := peer.NewDeliverClient(conn).Deliver(context.TODO())
	if err != nil {
		return nil, err
	}
	return &peerDeliverClient{client}, nil
}

type peerDeliverClient struct {
	delegate peer.Deliver_DeliverClient
}

func (c *peerDeliverClient) Send(envelope *cb.Envelope) error {
	return c.delegate.Send(envelope)
}
func (c *peerDeliverClient) Recv() (*cb.Block, *cb.Status, error) {
	msg, err := c.delegate.Recv()
	if err != nil {
		return nil, nil, err
	}
	switch t := msg.Type.(type) {
	case *peer.DeliverResponse_Status:
		return nil, &t.Status, nil
	case *peer.DeliverResponse_Block:
		return t.Block, nil, nil
	default:
		return nil, nil, errors.New("unexpected message")
	}
}
