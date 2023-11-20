package deliverclient

import (
	"context"

	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
)

type Provider struct{}

func (p *Provider) DeliverClient(conn *grpc.ClientConn) (deliverStream, error) {
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
func (c *ordererDeliverClient) CloseSend() error {
	return c.delegate.CloseSend()
}
