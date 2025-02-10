package broadcastdeliver

import (
	"context"
	"fmt"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

type (
	// DeliverCftClient allows delivering blocks from one connection at a time.
	// If one connection fails, it will try to connect to another one.
	DeliverCftClient struct {
		Connections   []*grpc.ClientConn
		Signer        protoutil.Signer
		ChannelID     string
		Retry         *connection.RetryProfile
		StreamCreator func(ctx context.Context, conn grpc.ClientConnInterface) (DeliverStream, error)
	}

	// DeliverConfig holds the configuration needed for deliver to run.
	DeliverConfig struct {
		StartBlkNum int64
		EndBlkNum   uint64
		OutputBlock chan<- *common.Block
	}

	// DeliverStream requires the following interface.
	DeliverStream interface {
		Send(*common.Envelope) error
		RecvBlockOrStatus() (*common.Block, *common.Status, error)
	}
)

// Deliver start receiving blocks starting from config.StartBlkNum to config.OutputBlock.
// The value of config.StartBlkNum is updated with the latest block number.
func (c *DeliverCftClient) Deliver(ctx context.Context, config *DeliverConfig) error {
	nodes := makeConnNodes(c.Connections, c.Retry)
	// We shuffle the nodes for load balancing.
	shuffle(nodes)
	for ctx.Err() == nil {
		if config.StartBlkNum > 0 && uint64(config.StartBlkNum) > config.EndBlkNum { // nolint:gosec
			return nil
		}
		curNode := nodes[0]
		err := c.receiveFromBlockDeliverer(ctx, curNode, config)
		logger.Infof("Error receiving blocks: %v", err)
		curNode.backoff()
		sortNodes(nodes)
	}
	return nil
}

func (c *DeliverCftClient) receiveFromBlockDeliverer(
	ctx context.Context, conn *connNode, config *DeliverConfig,
) error {
	err := conn.wait(ctx)
	if err != nil {
		return err
	}

	logger.Infof("Connecting to %s", conn.connection.Target())
	stream, err := c.StreamCreator(ctx, conn.connection)
	if err != nil {
		return err
	}

	logger.Infof("Sending seek request from block %d on channel %s.", config.StartBlkNum, c.ChannelID)
	seekEnv, err := seekSince(config.StartBlkNum, config.EndBlkNum, c.ChannelID, c.Signer)
	if err != nil {
		return errors.Wrap(err, "failed to create seek request")
	}
	if err := stream.Send(seekEnv); err != nil {
		return errors.Wrap(err, "failed to send seek request")
	}
	logger.Infof("Seek request sent.")

	// If we managed to send a request, we can reset the connection's backoff.
	conn.reset()

	outputBlock := channel.NewWriter(ctx, config.OutputBlock)
	for ctx.Err() == nil {
		block, status, err := stream.RecvBlockOrStatus()
		if err != nil {
			return err
		}
		if status != nil {
			return fmt.Errorf("disconnecting deliver service with status %s", status)
		}

		//nolint:gosec // integer overflow conversion uint64 -> int64
		config.StartBlkNum = int64(block.Header.Number) + 1
		logger.Infof("next expected block number is %d", config.StartBlkNum)
		outputBlock.Write(block)
	}

	return nil
}

// TODO: We have seek info only for the orderer but not for the ledger service. It needs
//       to implemented as fabric ledger also allows different seek info.

const (
	seekSinceOldestBlock = -2
	seekSinceNewestBlock = -1
)

var (
	oldest = &orderer.SeekPosition{Type: &orderer.SeekPosition_Oldest{Oldest: &orderer.SeekOldest{}}}
	newest = &orderer.SeekPosition{Type: &orderer.SeekPosition_Newest{Newest: &orderer.SeekNewest{}}}
)

func seekSince(
	startBlockNumber int64,
	endBlkNum uint64,
	channelID string,
	signer protoutil.Signer,
) (*common.Envelope, error) {
	var startPosition *orderer.SeekPosition
	switch startBlockNumber {
	case seekSinceOldestBlock:
		startPosition = oldest
	case seekSinceNewestBlock:
		startPosition = newest
	default:
		if startBlockNumber < -2 {
			return nil, errors.New("wrong seek value")
		}
		startPosition = &orderer.SeekPosition{Type: &orderer.SeekPosition_Specified{Specified: &orderer.SeekSpecified{
			Number: uint64(startBlockNumber), //nolint:gosec // integer overflow conversion int64 -> uint64
		}}}
	}

	if signer == nil {
		signer = &noOpSigner{}
	}

	return protoutil.CreateSignedEnvelope(common.HeaderType_DELIVER_SEEK_INFO, channelID, signer, &orderer.SeekInfo{
		Start: startPosition,
		Stop: &orderer.SeekPosition{Type: &orderer.SeekPosition_Specified{Specified: &orderer.SeekSpecified{
			Number: endBlkNum,
		}}},
		Behavior: orderer.SeekInfo_BLOCK_UNTIL_READY,
	}, 0, 0)
}
