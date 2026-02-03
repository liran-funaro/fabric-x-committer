/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package deliver

import (
	"context"
	"math"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric-x-common/common/util"
	"github.com/hyperledger/fabric-x-common/protoutil"

	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/logging"
)

type (
	// deliveryParameters holds the parameters for delivery.
	deliveryParameters struct {
		streamCreator           func(ctx context.Context) (streamer, error)
		channelID               string
		signer                  protoutil.Signer
		nextBlockNum            uint64
		outputBlock             chan<- *common.Block
		outputBlockWithSourceID chan<- *BlockWithSourceID
		headerOnly              bool
		sourceID                uint32
		retry                   *connection.RetryProfile
	}

	// streamer requires the following interface.
	streamer interface {
		Context() context.Context
		Send(*common.Envelope) error
		RecvBlockOrStatus() (*common.Block, *common.Status, error)
	}
)

// MaxBlockNum is used for endless deliver.
const MaxBlockNum uint64 = math.MaxUint64

var logger = logging.New("deliver")

// toChannel connects to a delivery server and delivers the stream for
// the specified channel-id to a go channel.
// Returns the next block number to be requested after delivery ends.
// It returns when an error occurs or when the context is done.
// It will attempt to reconnect on errors.
func toChannel(ctx context.Context, p deliveryParameters) error {
	return connection.Sustain(ctx, p.retry, func() error {
		return toChannelWithoutReconnect(ctx, &p)
	})
}

// toChannelWithoutReconnect connects to a delivery server and delivers the stream for
// the specified channel-id to a go channel.
// The value of p.nextBlockNum is updated with the latest block number.
// It returns when an error occurs or when the context is done.
// It will not attempt to reconnect on errors.
func toChannelWithoutReconnect(ctx context.Context, p *deliveryParameters) error {
	seekEnv, seekErr := seekSince(p)
	if seekErr != nil {
		return errors.Join(connection.ErrNonRetryable, errors.Wrap(seekErr, "failed to create seek request"))
	}

	// We create a new context per stream to ensure it cancels on error.
	cCtx, sCancel := context.WithCancel(ctx)
	defer sCancel()
	stream, streamErr := p.streamCreator(cCtx)
	if streamErr != nil {
		return errors.Join(connection.ErrBackOff, errors.Wrap(streamErr, "failed to create stream"))
	}
	sCtx := stream.Context()

	//nolint:contextcheck // false positive (stream's context is inherited from cCtx).
	addr := util.ExtractRemoteAddress(sCtx)
	logger.Infof("Deliver connected to %s", addr)

	logger.Infof("Sending seek request from block %d on channel %s.", p.nextBlockNum, p.channelID)
	if err := stream.Send(seekEnv); err != nil {
		return errors.Join(connection.ErrBackOff, errors.Wrap(err, "failed to send seek request"))
	}

	//nolint:contextcheck // false positive (stream's context is inherited from cCtx).
	outputBlock := channel.NewWriter(sCtx, p.outputBlock)
	//nolint:contextcheck // false positive (stream's context is inherited from cCtx).
	outputBlockWithSourceID := channel.NewWriter(sCtx, p.outputBlockWithSourceID)

	// Initially, backoff on error. But upon receiving fhe first block, we reset the backoff.
	backoff := connection.ErrBackOff
	for sCtx.Err() == nil {
		logger.Debugf("Next expected block number is %d", p.nextBlockNum)
		block, status, err := stream.RecvBlockOrStatus()
		if err != nil {
			return errors.Join(backoff, errors.Wrap(err, "failed to receive block"))
		}
		if status != nil {
			return errors.Join(backoff, errors.Newf("delivery status: %s", status.String()))
		}

		// We make minimal verifications to ensure we receive blocks in order.
		// This allows us to restart the connection from the next expected block.
		// We restart the connection upon failure.
		if block == nil || block.Header == nil {
			return errors.Join(backoff, errors.New("received nil block or with nil header"))
		}
		if block.Header.Number != p.nextBlockNum {
			return errors.Join(backoff, errors.Errorf("received block number %d != %d (expected)",
				block.Header.Number, p.nextBlockNum))
		}
		p.nextBlockNum++
		backoff = nil

		// Write will be no-op if the output buffer is nil.
		outputBlock.Write(block)
		outputBlockWithSourceID.Write(&BlockWithSourceID{
			SourceID: p.sourceID,
			Block:    block,
		})
	}
	return errors.Wrap(sCtx.Err(), "context ended")
}

// TODO: We have seek info only for the orderer but not for the ledger service. It needs
//       to implemented as fabric ledger also allows different seek info.

func seekSince(p *deliveryParameters) (*common.Envelope, error) {
	contentType := orderer.SeekInfo_BLOCK
	if p.headerOnly {
		contentType = orderer.SeekInfo_HEADER_WITH_SIG
	}
	return protoutil.CreateSignedEnvelopeWithCert(
		common.HeaderType_DELIVER_SEEK_INFO, p.channelID, p.signer, &orderer.SeekInfo{
			Start:       newSeekPosition(p.nextBlockNum),
			Stop:        newSeekPosition(MaxBlockNum),
			Behavior:    orderer.SeekInfo_BLOCK_UNTIL_READY,
			ContentType: contentType,
		}, 0, 0,
	)
}

func newSeekPosition(nextBlockNum uint64) *orderer.SeekPosition {
	return &orderer.SeekPosition{Type: &orderer.SeekPosition_Specified{Specified: &orderer.SeekSpecified{
		Number: nextBlockNum,
	}}}
}
