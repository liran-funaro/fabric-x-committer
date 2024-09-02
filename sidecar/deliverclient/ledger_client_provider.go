package deliverclient

import (
	"context"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
)

func newLedgerDeliverStream(ctx context.Context, conn *grpc.ClientConn) (*ledgerDeliverStream, error) {
	client, err := peer.NewDeliverClient(conn).Deliver(ctx)
	if err != nil {
		return nil, err
	}
	return &ledgerDeliverStream{client}, nil
}

// ledgerDeliverStream implements deliverStream.
type ledgerDeliverStream struct {
	peer.Deliver_DeliverClient
}

// RecvBlockOrStatus receives the committed block from the ledger service. The first
// block number to be received is dependent on the seek position
// sent in DELIVER_SEEK_INFO message.
func (s *ledgerDeliverStream) RecvBlockOrStatus() (*common.Block, *common.Status, error) {
	msg, err := s.Recv()
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
