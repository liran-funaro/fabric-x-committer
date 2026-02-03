/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package deliver

import (
	"context"
	"crypto/rand"
	"maps"
	"math"
	"math/big"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"google.golang.org/grpc"

	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/ordererconn"
)

type (
	// OrdererDeliveryParameters needed for deliver to run.
	OrdererDeliveryParameters struct {
		FaultToleranceLevel string
		TLS                 connection.TLSConfig
		Retry               *connection.RetryProfile
		Identity            *ordererconn.IdentityConfig

		OutputBlock             chan<- *common.Block
		OutputBlockWithSourceID chan<- *BlockWithSourceID
		BlockWithholdingTimeout time.Duration
		LivenessCheckInterval   time.Duration

		// These fields are updated by the end of the delivery process.
		// NextBlockVerificationConfig dictates how we will process the next incoming block,
		// that comes after the last processed block (LastBlock).
		// However, we might know of a newer config (LastestKnownConfig) block that has yet to be processed.
		// This can be either due to information from the user configuration, via previous delivery run,
		// or when the headers only delivery was ahead of the data blocks.
		// This can help in cases where the delivery was down too long and missed a crucial config-block
		// that updated all the endpoints, leaving no known orderers to fetch blocks from.
		// Note that the genesis block should NOT be passed as the LastBlock for two reasons:
		//   1. Allowing the delivery to deliver it for processing.
		//   2. The genesis block might not be signed properly, however, it will be signed
		//      when delivered from the orderer.
		NextBlockVerificationConfig *common.Block
		LastestKnownConfig          *common.Block
		LastBlock                   *common.Block
	}

	// BlockWithSourceID can provide extra information on the block source when aggregating multiple
	// sources into a single output channel.
	BlockWithSourceID struct {
		SourceID uint32
		Block    *common.Block
	}

	// ftDelivery supports fault tolerance delivery.
	ftDelivery struct {
		params                  *OrdererDeliveryParameters
		monitorBlockWithholding bool
		jointOutputBlock        chan *BlockWithSourceID
		signer                  protoutil.Signer

		// dataStream and headerOnlyStream holds the processing state of each stream respectively.
		// They progress in parallel independently.
		dataStream       blockProcessingState
		headerOnlyStream blockProcessingState

		// latestConfig holds the latest known config. It is used to have the most
		// up-to-date channel ID and orderer endpoints when reconnecting.
		// For verification, each blockProcessingState holds its own configState.
		latestConfig configState

		dataBlockDeadline map[uint64]time.Time

		curDataBlockID uint32
		wg             sync.WaitGroup
		cancelWorkers  context.CancelFunc
		activeWorkers  atomic.Int64
	}

	streamAction uint
)

const (
	streamActionNone streamAction = iota
	streamActionRestart
	streamActionStalemate
)

// OrdererToChannel verifies blocks from the input channel and sends
// verified data blocks to the output channel.
// It returns when the input channel is closed or when the context is done.
func OrdererToChannel(ctx context.Context, odp *OrdererDeliveryParameters) error {
	d, err := newFTDelivery(odp)
	if err != nil {
		return err
	}
	defer d.finalize()

	cCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = d.run(cCtx)

	// Update input parameters for future runs.
	odp.LastBlock = d.dataStream.lastBlock
	odp.NextBlockVerificationConfig = d.dataStream.configBlock
	odp.LastestKnownConfig = d.latestConfig.configBlock
	return err
}

func newFTDelivery(odp *OrdererDeliveryParameters) (*ftDelivery, error) {
	initDefaultParameters(odp)
	signer, err := ordererconn.NewIdentitySigner(odp.Identity)
	if err != nil {
		return nil, errors.Wrap(err, "error creating identity signer")
	}
	ft := odp.FaultToleranceLevel
	d := &ftDelivery{
		params:                  odp,
		signer:                  signer,
		jointOutputBlock:        make(chan *BlockWithSourceID, cap(odp.OutputBlock)),
		dataBlockDeadline:       make(map[uint64]time.Time),
		monitorBlockWithholding: ft == ordererconn.BFT,
		dataStream: blockProcessingState{
			dataBlockStream:     true,
			verifyBlocksContent: ft == ordererconn.BFT || ft == ordererconn.CFT,
		},
	}
	latestUpdated, err := d.latestConfig.updateIfConfigBlock(odp.LastestKnownConfig)
	if err != nil {
		return nil, err
	}
	nextUpdated, err := d.dataStream.updateIfConfigBlock(odp.NextBlockVerificationConfig)
	if err != nil {
		return nil, err
	}
	if nextUpdated && !latestUpdated {
		d.latestConfig = d.dataStream.configState
	}
	if latestUpdated && !nextUpdated {
		d.dataStream.configState = d.latestConfig
	}
	// We assert that the latest config is indeed the latest.
	// This is useful in cases that the latest config block was given by the user,
	// but the system have a newer config block in the ledger.
	if d.latestConfig.configBlockNumber < d.dataStream.configBlockNumber {
		d.latestConfig = d.dataStream.configState
	}

	// We process the last block and start the following blocks processing from it.
	if odp.LastBlock != nil && odp.LastBlock.Header != nil {
		d.dataStream.nextBlockNum = odp.LastBlock.Header.Number
		verErr := d.dataStream.verificationStepAndUpdateState(&BlockWithSourceID{
			Block: odp.LastBlock,
			// We use a large ID to ensure we do not confuse it with a real source.
			SourceID: math.MaxInt32,
		})
		if verErr != nil {
			return nil, verErr
		}
	}

	// We validate that the config block is ahead the next expected block to fail fast.
	if d.dataStream.nextBlockNum < d.dataStream.configBlockNumber {
		return nil, errors.Errorf("Config block number [%d] is ahead of the next expected block [%d]",
			d.dataStream.configBlockNumber, d.dataStream.nextBlockNum)
	}

	// We start the headers-only streams from the same point as the data stream.
	d.headerOnlyStream = d.dataStream
	d.headerOnlyStream.dataBlockStream = false
	return d, nil
}

func initDefaultParameters(odp *OrdererDeliveryParameters) {
	if odp.BlockWithholdingTimeout == 0 {
		odp.BlockWithholdingTimeout = time.Second
	}
	if odp.LivenessCheckInterval == 0 {
		odp.LivenessCheckInterval = time.Second
	}
	odp.FaultToleranceLevel = strings.ToUpper(odp.FaultToleranceLevel)
	if odp.FaultToleranceLevel == ordererconn.UnspecifiedFT {
		odp.FaultToleranceLevel = ordererconn.DefaultFT
	}
}

func (d *ftDelivery) finalize() {
	if d.cancelWorkers != nil {
		// Cancel workers if any.
		d.cancelWorkers()
	}
	d.wg.Wait()
}

func (d *ftDelivery) run(ctx context.Context) error {
	d.updateDeliveryStreams(ctx, streamActionRestart)
	deliveryBlockOutput := channel.NewWriter(ctx, d.params.OutputBlock)
	deliveryBlockOutputWithSourceID := channel.NewWriter(ctx, d.params.OutputBlockWithSourceID)

	for ctx.Err() == nil {
		deliverBlock, action := d.processNextBlock(ctx)
		if deliverBlock != nil {
			// Write will be no-op if the output buffer is nil.
			deliveryBlockOutput.Write(deliverBlock.Block)
			deliveryBlockOutputWithSourceID.Write(deliverBlock)
		}
		if action == streamActionStalemate {
			return errors.New("delivery sources are not available")
		}
		d.updateDeliveryStreams(ctx, action)
	}
	return errors.Wrap(ctx.Err(), "context ended")
}

func (d *ftDelivery) processNextBlock(ctx context.Context) (deliverBlock *BlockWithSourceID, action streamAction) {
	blk := d.readNextBlock(ctx)
	if blk == nil { // Timeout occurred.
		action = d.checkBlockWithholding()
		// Timeout means there are no more blocks in the queue.
		// If we have no running delivery streams, the queue won't
		// be filled, and no progress can be made.
		// However, if we need to restart the streams, we might recover.
		if action == streamActionNone && d.activeWorkers.Load() == 0 {
			action = streamActionStalemate
		}
		return nil, action
	}

	// We pick the processing state according to the block source (data or header only).
	state := d.getProcessingState(blk)

	// Verification step: a block is ignored if it failed verification.
	verificationErr := state.verificationStepAndUpdateState(blk)
	if verificationErr != nil {
		// The underlying delivery client ensures consecutive blocks.
		// However, the join stream can have blocks out-of-ourder for two reasons.
		//   1. We switched the data stream source, but the queue had some previously unprocessed blocks.
		//      Thus, by the time the first block arrives from the new stream, the actual state might be
		//      ahead of requested next block.
		//   2. We aggregate multiple simultaneous header-only streams into a single channel.
		//      Thus, we get the same block multiple times from different sources.
		// In both cases, we process the first block that arrives in order, and skip the rest.
		// With that, we save processing time of duplicated blocks from different sources.
		// Since getting an unexpected block number from a source other than the last updater is a
		// correct behavior, we don't need to log it.
		if !errors.Is(verificationErr, ErrUnexpectedBlockNumber) || state.updaterID == blk.SourceID {
			logger.Warnf("Block verification failed for source [%d] (data=%v): %v",
				blk.SourceID, state.dataBlockStream, verificationErr)
		}

		// An issue on the main data block stream, requires a restart
		// to ensure progress.
		// However, if the headers-only streams have issues, we don't restart the streams
		// as this can lead to a DOS attack by a malicious party.
		if state.dataBlockStream {
			return nil, streamActionRestart
		}
		return nil, streamActionNone
	}

	// Deliver step: if it is a valid data block, deliver it.
	if state.dataBlockStream {
		deliverBlock = blk
	}

	// Config update step: if it is a valid block, apply config updates.
	return deliverBlock, d.updateIfConfigBlockStep(blk, state)
}

func (d *ftDelivery) readNextBlock(ctx context.Context) *BlockWithSourceID {
	timeout := d.params.LivenessCheckInterval
	nextBlockDeadline, ok := d.dataBlockDeadline[d.dataStream.nextBlockNum]
	if ok {
		timeout = min(timeout, time.Until(nextBlockDeadline))
	}
	select {
	case blk := <-d.jointOutputBlock:
		return blk
	case <-ctx.Done():
		return nil
	case <-time.After(timeout):
		return nil
	}
}

func (d *ftDelivery) getProcessingState(block *BlockWithSourceID) *blockProcessingState {
	switch block.SourceID {
	case d.curDataBlockID:
		return &d.dataStream
	default:
		return &d.headerOnlyStream
	}
}

func (d *ftDelivery) updateIfConfigBlockStep(blk *BlockWithSourceID, state *blockProcessingState) streamAction {
	configUpdated, configUpdateErr := state.updateIfConfigBlock(blk.Block)
	if configUpdateErr != nil {
		// At this point, the block is valid and delivered.
		// We can only log the issue for investigative purposes.
		logger.Warnf("failed to update config block: %v", configUpdateErr)
	}

	if configUpdated && d.latestConfig.configBlockNumber < state.configBlockNumber {
		// If a newer config block appears, it may contain endpoints update.
		// So we restart the streams with the latest config.
		d.latestConfig = state.configState
		return streamActionRestart
	}

	return d.checkBlockWithholding()
}

func (d *ftDelivery) checkBlockWithholding() streamAction {
	if !d.monitorBlockWithholding {
		return streamActionNone
	}

	// If the data block source is ahead of the headers, we are fine.
	if d.dataStream.nextBlockNum >= d.headerOnlyStream.nextBlockNum {
		clear(d.dataBlockDeadline)
		return streamActionNone
	}

	// If a new header was received, we add the deadline for the block.
	// The second condition is to assert that we can subtract one from the next block.
	if d.headerOnlyStream.updated && d.headerOnlyStream.nextBlockNum > 0 {
		lastHeaderBlock := d.headerOnlyStream.nextBlockNum - 1
		d.dataBlockDeadline[lastHeaderBlock] = time.Now().Add(d.params.BlockWithholdingTimeout)
		d.headerOnlyStream.updated = false
	}

	// If a new data block was received, we clear the deadline for the block.
	// The second condition is to assert that we can subtract one from the next block.
	if d.dataStream.updated && d.dataStream.nextBlockNum > 0 {
		// If there was no deadline, then "delete" is no-op.
		delete(d.dataBlockDeadline, d.dataStream.nextBlockNum-1)
		d.dataStream.updated = false
	}

	// Check if we have a deadline for the next data block.
	deadline, ok := d.dataBlockDeadline[d.dataStream.nextBlockNum]
	if !ok || time.Now().Before(deadline) {
		// We still have time.
		return streamActionNone
	}

	logger.Warnf("Suspecting block withholding by party ID [%d]. Replacing data block source.",
		d.curDataBlockID)

	// We increase the deadline to allow the system to converge after restart.
	d.dataBlockDeadline[d.dataStream.nextBlockNum] = deadline.Add(d.params.BlockWithholdingTimeout)

	// We restart the delivery with the most advanced source as the data block source.
	return streamActionRestart
}

func (d *ftDelivery) updateDeliveryStreams(ctx context.Context, action streamAction) {
	if action == streamActionNone && d.cancelWorkers != nil {
		return
	}

	logger.Infof("Staring delivery streams...")

	// Cancel workers if any. We don't wait for them to finish.
	// It may cause duplicated blocks in the stream.
	// But it is OK since, we only verify the first received instance of each block.
	if d.cancelWorkers != nil {
		d.cancelWorkers()
	}
	dCtx, cancel := context.WithCancel(ctx)
	d.cancelWorkers = cancel

	// Clear block withholding status.
	clear(d.dataBlockDeadline)
	d.dataStream.updated = false
	d.headerOnlyStream.updated = false

	switch d.monitorBlockWithholding {
	case true:
		d.deliveryStreamsWithMonitor(dCtx)
	default:
		d.singleDeliveryStream(dCtx)
	}
}

func (d *ftDelivery) singleDeliveryStream(ctx context.Context) {
	d.curDataBlockID = 0
	d.startSingleDeliveryStream(ctx, d.latestConfig.deliverEndpoints, deliveryParameters{
		nextBlockNum: d.dataStream.nextBlockNum,
		headerOnly:   false,
		sourceID:     d.curDataBlockID,
	})
}

func (d *ftDelivery) deliveryStreamsWithMonitor(ctx context.Context) {
	d.curDataBlockID = d.pickDataBlockStreamID()
	logger.Infof("Using party ID [%d] as the data block source", d.curDataBlockID)
	for id, endpoints := range d.latestConfig.deliverEndpointsPerID {
		isDataBlock := id == d.curDataBlockID
		deliverParams := deliveryParameters{
			nextBlockNum: d.headerOnlyStream.nextBlockNum,
			headerOnly:   !isDataBlock,
			sourceID:     id,
		}
		if isDataBlock {
			deliverParams.nextBlockNum = d.dataStream.nextBlockNum
		}
		d.startSingleDeliveryStream(ctx, endpoints, deliverParams)
	}
}

func (d *ftDelivery) startSingleDeliveryStream(
	ctx context.Context, endpoints []*connection.Endpoint, dp deliveryParameters,
) {
	d.activeWorkers.Add(1)
	d.wg.Go(func() {
		defer d.activeWorkers.Add(-1)
		conn, connErr := connection.NewLoadBalancedConnection(&connection.MultiClientConfig{
			Endpoints: endpoints,
			Retry:     d.params.Retry,
			TLS:       d.params.TLS,
		})
		if connErr != nil {
			logger.Errorf("Delivery worker connection failed with error: %v", connErr)
			return
		}
		defer connection.CloseConnectionsLog(conn)

		// Fill common parameters.
		dp.streamCreator = ordererStreamCreator(conn)
		dp.channelID = d.latestConfig.channelHeader.ChannelId
		dp.signer = d.signer
		dp.outputBlockWithSourceID = d.jointOutputBlock

		err := toChannel(ctx, dp)
		if err != nil {
			logger.Errorf("Delivery worker ended with error: %v", err)
		}
	})
}

func ordererStreamCreator(conn *grpc.ClientConn) func(ctx context.Context) (streamer, error) {
	client := orderer.NewAtomicBroadcastClient(conn)
	return func(ctx context.Context) (streamer, error) {
		deliverStream, deliverErr := client.Deliver(ctx)
		if deliverErr != nil {
			return nil, deliverErr
		}
		return &ordererDeliverStream{AtomicBroadcast_DeliverClient: deliverStream}, nil
	}
}

func (d *ftDelivery) pickDataBlockStreamID() uint32 {
	// If the headers only stream is ahead, and it uses the latest config block (as expected),
	// we use its latest updater.
	if d.headerOnlyStream.nextBlockNum > d.dataStream.nextBlockNum &&
		d.latestConfig.configBlockNumber == d.headerOnlyStream.configBlockNumber {
		return d.headerOnlyStream.updaterID
	}
	parties := slices.Collect(maps.Keys(d.latestConfig.deliverEndpointsPerID))
	// crypto/rand works with big.Int.
	// It returns a value in the range [0, n).
	idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(parties))))
	if err != nil {
		return parties[0]
	}
	return parties[idx.Int64()]
}

// ordererDeliverStream implements deliver.streamer.
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
