/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package deliver

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"

	"github.com/hyperledger/fabric-x-committer/utils/connection"
)

// CommitterDeliveryParameters needed for deliver to run.
// This is copy of deliver.Parameters to allow easy divergence in the future.
type CommitterDeliveryParameters struct {
	Client       *connection.ClientConfig
	ChannelID    string
	NextBlockNum uint64
	OutputBlock  chan<- *common.Block
}

// CommiterToChannel start receiving blocks starting from nextBlockNum to outputBlock.
// The value of nextBlockNum is updated with the latest block number.
func CommiterToChannel(ctx context.Context, cdp CommitterDeliveryParameters) error {
	conn, err := connection.NewSingleConnection(cdp.Client)
	if err != nil {
		return err
	}
	defer connection.CloseConnectionsLog(conn)
	client := peer.NewDeliverClient(conn)
	return toChannel(ctx, deliveryParameters{
		channelID:    cdp.ChannelID,
		nextBlockNum: cdp.NextBlockNum,
		outputBlock:  cdp.OutputBlock,
		streamCreator: func(ctx context.Context) (streamer, error) {
			deliverStream, deliverErr := client.Deliver(ctx)
			if deliverErr != nil {
				return nil, deliverErr
			}
			return &ledgerDeliverStream{Deliver_DeliverClient: deliverStream}, nil
		},
	})
}

// ledgerDeliverStream implements deliver.streamer.
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
