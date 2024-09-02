package deliverclient

import (
	"context"
	"flag"
	"math"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/identity"
	"google.golang.org/grpc"
)

func newOrdererDeliverStream(ctx context.Context, conn *grpc.ClientConn) (*ordererDeliverStream, error) {
	client, err := orderer.NewAtomicBroadcastClient(conn).Deliver(ctx)
	if err != nil {
		return nil, err
	}
	return &ordererDeliverStream{client}, nil
}

// ordererDeliverStream implements deliverStream.
type ordererDeliverStream struct {
	orderer.AtomicBroadcast_DeliverClient
}

// RecvBlockOrStatus receives the created block from the ordering service. The first
// block number to be received is dependent on the seek position
// sent in DELIVER_SEEK_INFO message.
func (c *ordererDeliverStream) RecvBlockOrStatus() (*common.Block, *common.Status, error) {
	msg, err := c.Recv()
	if err != nil {
		return nil, nil, err
	}
	switch t := msg.Type.(type) {
	case *orderer.DeliverResponse_Status:
		return nil, &t.Status, nil
	case *orderer.DeliverResponse_Block:
		return t.Block, nil, nil
	default:
		return nil, nil, errors.New("unexpected message")
	}
}

// TODO: We have seek info only for the orderer but not for the ledger service. It needs
//       to implemented as fabric ledger also allows different seek info.

const (
	seekSinceOldestBlock = -2
	seekSinceNewestBlock = -1
)

var (
	oldest  = &orderer.SeekPosition{Type: &orderer.SeekPosition_Oldest{Oldest: &orderer.SeekOldest{}}}
	newest  = &orderer.SeekPosition{Type: &orderer.SeekPosition_Newest{Newest: &orderer.SeekNewest{}}}
	maxStop = &orderer.SeekPosition{Type: &orderer.SeekPosition_Specified{Specified: &orderer.SeekSpecified{
		Number: math.MaxUint64,
	}}}
)

func seekSince(startBlockNumber int64, cID string, signer identity.SignerSerializer) *common.Envelope {
	env, err := protoutil.CreateSignedEnvelope(common.HeaderType_DELIVER_SEEK_INFO, cID, signer, &orderer.SeekInfo{
		Start:    seekPosition(startBlockNumber),
		Stop:     maxStop,
		Behavior: orderer.SeekInfo_BLOCK_UNTIL_READY,
	}, 0, 0)
	if err != nil {
		panic(err)
	}
	return env
}

func seekPosition(startBlockNumber int64) *orderer.SeekPosition {
	var startPosition *orderer.SeekPosition
	switch startBlockNumber {
	case seekSinceOldestBlock:
		startPosition = oldest
	case seekSinceNewestBlock:
		startPosition = newest
	default:
		if startBlockNumber < -2 {
			flag.PrintDefaults()
			panic(errors.New("wrong seek value"))
		}
		startPosition = &orderer.SeekPosition{Type: &orderer.SeekPosition_Specified{Specified: &orderer.SeekSpecified{
			Number: uint64(startBlockNumber),
		}}}
	}

	return startPosition
}
