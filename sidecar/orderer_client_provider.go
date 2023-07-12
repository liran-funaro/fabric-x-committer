package sidecar

import (
	"context"

	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/deliver"
	"google.golang.org/grpc"
)

type OrdererDeliverClientProvider struct{}

func (p *OrdererDeliverClientProvider) DeliverClient(conn *grpc.ClientConn) (deliver.Client, error) {
	client, err := ab.NewAtomicBroadcastClient(conn).Deliver(context.TODO())
	if err != nil {
		return nil, err
	}
	return &ordererDeliverClient{client}, nil
}

type ordererDeliverClient struct {
	delegate ab.AtomicBroadcast_DeliverClient
}

func (c *ordererDeliverClient) Send(envelope *cb.Envelope) error {
	return c.delegate.Send(envelope)
}
func (c *ordererDeliverClient) Recv() (*cb.Block, *cb.Status, error) {
	msg, err := c.delegate.Recv()
	if err != nil {
		return nil, nil, err
	}
	switch t := msg.Type.(type) {
	case *ab.DeliverResponse_Status:
		return nil, &t.Status, nil
	case *ab.DeliverResponse_Block:
		return t.Block, nil, nil
	default:
		return nil, nil, errors.New("unexpected message")
	}
}
