/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sidecar

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"golang.org/x/sync/errgroup"

	"github.com/hyperledger/fabric-x-committer/api/protoblocktx"
	"github.com/hyperledger/fabric-x-committer/api/protocoordinatorservice"
	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring/promutil"
)

type (
	relay struct {
		incomingBlockToBeCommitted    <-chan *common.Block
		outgoingCommittedBlock        chan<- *common.Block
		nextBlockNumberToBeCommitted  atomic.Uint64
		activeBlocksCount             atomic.Int32
		blkNumToBlkWithStatus         utils.SyncMap[uint64, *blockWithStatus]
		txIDToBlkNum                  utils.SyncMap[string, uint64]
		lastCommittedBlockSetInterval time.Duration
		waitingTxsSlots               *utils.Slots
		metrics                       *perfMetrics
	}

	blockWithStatus struct {
		block        *common.Block
		txStatus     []validationCode
		txIDToTxNum  map[string]int
		pendingCount int
	}

	relayRunConfig struct {
		coordClient                    protocoordinatorservice.CoordinatorClient
		nextExpectedBlockByCoordinator uint64
		configUpdater                  func(*common.Block)
		incomingBlockToBeCommitted     <-chan *common.Block
		outgoingCommittedBlock         chan<- *common.Block
		waitingTxsLimit                int
	}
)

const defaultLastCommittedBlockSetInterval = 5 * time.Second

func newRelay(
	lastCommittedBlockSetInterval time.Duration,
	metrics *perfMetrics,
) *relay {
	logger.Info("Initializing new relay")
	if lastCommittedBlockSetInterval == 0 {
		lastCommittedBlockSetInterval = defaultLastCommittedBlockSetInterval
	}
	return &relay{
		lastCommittedBlockSetInterval: lastCommittedBlockSetInterval,
		metrics:                       metrics,
	}
}

// run starts the relay service. The call to run blocks until an error occurs or the context is canceled.
func (r *relay) run(ctx context.Context, config *relayRunConfig) error { //nolint:contextcheck // false positive
	r.nextBlockNumberToBeCommitted.Store(config.nextExpectedBlockByCoordinator)
	r.incomingBlockToBeCommitted = config.incomingBlockToBeCommitted
	r.outgoingCommittedBlock = config.outgoingCommittedBlock
	r.blkNumToBlkWithStatus.Clear()
	r.txIDToBlkNum.Clear()
	r.waitingTxsSlots = utils.NewSlots(int64(config.waitingTxsLimit))

	rCtx, rCancel := context.WithCancel(ctx)
	defer rCancel()

	// Using the errgroup context for the stream ensures that we cancel the stream once one of the tasks fails.
	// And we use the stream's context to ensure that if the stream is closed, we stop all the tasks.
	g, gCtx := errgroup.WithContext(rCtx)
	stream, err := config.coordClient.BlockProcessing(gCtx)
	if err != nil {
		return errors.Wrap(err, "failed to open stream for block processing")
	}
	sCtx := stream.Context()

	logger.Infof("Starting coordinator sender and receiver")

	expectedNextBlockToBeCommitted := r.nextBlockNumberToBeCommitted.Load()

	mappedBlockQueue := make(chan *scBlockWithStatus, cap(r.incomingBlockToBeCommitted))
	g.Go(func() error {
		return r.preProcessBlock(sCtx, mappedBlockQueue, config.configUpdater)
	})
	g.Go(func() error {
		return r.sendBlocksToCoordinator(sCtx, mappedBlockQueue, stream)
	})

	statusBatch := make(chan *protoblocktx.TransactionsStatus, cap(r.outgoingCommittedBlock))
	g.Go(func() error {
		return receiveStatusFromCoordinator(sCtx, stream, statusBatch)
	})
	g.Go(func() error {
		return r.processStatusBatch(sCtx, statusBatch)
	})

	g.Go(func() error {
		return r.setLastCommittedBlockNumber(sCtx, config.coordClient, expectedNextBlockToBeCommitted)
	})

	return utils.ProcessErr(g.Wait(), "stream with the coordinator has ended")
}

func (r *relay) preProcessBlock(
	ctx context.Context,
	mappedBlockQueue chan<- *scBlockWithStatus,
	configUpdater func(*common.Block),
) error {
	incomingBlockToBeCommitted := channel.NewReader(ctx, r.incomingBlockToBeCommitted)
	queue := channel.NewWriter(ctx, mappedBlockQueue)

	done := context.AfterFunc(ctx, r.waitingTxsSlots.Broadcast)
	defer done()

	for {
		block, ok := incomingBlockToBeCommitted.Read()
		if !ok {
			return errors.Wrap(ctx.Err(), "context ended")
		}
		if block.Header == nil {
			logger.Warn("Received a block without header")
			continue
		}

		logger.Debugf("Block %d arrived in the relay", block.Header.Number)

		start := time.Now()
		mappedBlock := mapBlock(block)
		promutil.Observe(r.metrics.blockMappingInRelaySeconds, time.Since(start))
		if mappedBlock.isConfig {
			configUpdater(block)
		}

		txsCount := len(mappedBlock.block.Txs)
		r.waitingTxsSlots.Acquire(ctx, int64(txsCount))
		promutil.AddToGauge(r.metrics.waitingTransactionsQueueSize, txsCount)
		queue.Write(mappedBlock)
	}
}

func (r *relay) sendBlocksToCoordinator(
	ctx context.Context,
	mappedBlockQueue <-chan *scBlockWithStatus,
	stream protocoordinatorservice.Coordinator_BlockProcessingClient,
) error {
	queue := channel.NewReader(ctx, mappedBlockQueue)
	outgoingCommittedBlock := channel.NewWriter(ctx, r.outgoingCommittedBlock)

	for {
		mappedBlock, ok := queue.Read()
		if !ok {
			return errors.Wrap(ctx.Err(), "context ended")
		}

		startTime := time.Now()
		blockNum := mappedBlock.block.Number
		r.blkNumToBlkWithStatus.Store(blockNum, mappedBlock.withStatus)

		dupIdx := make([]int, 0, len(mappedBlock.block.Txs))
		for i, tx := range mappedBlock.block.Txs {
			if _, loaded := r.txIDToBlkNum.LoadOrStore(tx.Id, blockNum); !loaded {
				continue
			}
			logger.Debugf("txID [%s] is duplicate", tx.Id)
			promutil.AddToCounterVec(r.metrics.transactionsStatusReceivedTotal, []string{
				protoblocktx.Status_REJECTED_DUPLICATE_TX_ID.String(),
			}, 1)
			mappedBlock.withStatus.pendingCount--
			txNum := mappedBlock.block.TxsNum[i]
			mappedBlock.withStatus.txStatus[txNum] = validationCode(protoblocktx.Status_REJECTED_DUPLICATE_TX_ID)
			dupIdx = append(dupIdx, i)
		}

		// Iterate over the indices in reverse order. Note that the dupIdx is sorted by default.
		for _, index := range slices.Backward(dupIdx) {
			mappedBlock.block.Txs = slices.Delete(mappedBlock.block.Txs, index, index+1)
			mappedBlock.block.TxsNum = slices.Delete(mappedBlock.block.TxsNum, index, index+1)
		}

		r.activeBlocksCount.Add(1)

		if mappedBlock.withStatus.pendingCount == 0 {
			r.processCommittedBlocksInOrder(ctx, outgoingCommittedBlock)
		}

		if err := stream.Send(mappedBlock.block); err != nil {
			return errors.Wrap(err, "failed to send a block to the coordinator")
		}
		txsCount := len(mappedBlock.block.Txs)
		promutil.AddToCounter(r.metrics.transactionsSentTotal, txsCount)
		logger.Debugf("Sent SC block %d with %d transactions to Coordinator",
			mappedBlock.block.Number, txsCount)
		promutil.Observe(r.metrics.mappedBlockProcessingInRelaySeconds, time.Since(startTime))
	}
}

func receiveStatusFromCoordinator(
	ctx context.Context,
	stream protocoordinatorservice.Coordinator_BlockProcessingClient,
	statusBatch chan<- *protoblocktx.TransactionsStatus,
) error {
	txsStatus := channel.NewWriter(ctx, statusBatch)
	for {
		response, err := stream.Recv()
		if err != nil {
			return errors.Wrap(err, "failed to receive statuses from the coordinator")
		}
		logger.Debugf("Received status batch (%d updates) from coordinator", len(response.GetStatus()))

		txsStatus.Write(response)
	}
}

func (r *relay) processStatusBatch(
	ctx context.Context,
	statusBatch <-chan *protoblocktx.TransactionsStatus,
) error {
	txsStatus := channel.NewReader(ctx, statusBatch)
	outgoingCommittedBlock := channel.NewWriter(ctx, r.outgoingCommittedBlock)
	for {
		tStatus, readOK := txsStatus.Read()
		if !readOK {
			return errors.Wrap(ctx.Err(), "context ended")
		}

		txStatusProcessedCount := int64(0)
		startTime := time.Now()
		for txID, txStatus := range tStatus.Status {
			if blockNum, ok := r.txIDToBlkNum.Load(txID); !ok || txStatus.BlockNumber != blockNum {
				// - Case 1: Block not found.
				//   Consider a scenario where the connection between the sidecar and the coordinator fails due
				//   to a network issue—not because the coordinator restarts. Assume the relay has already submitted
				//   a block to the coordinator before the connection issue occurs.
				//   When the connection is re-established and execution resumes, we will receive the statuses of
				//   transactions submitted before the connectivity issue. However, the relay will no longer track
				//   these transactions. This is because when the connection fails, the relay returns control to
				//   the sidecar, which then fetches statuses directly using the gRPC API to recover the block store
				//   once the connection is re-established. Consequently, the relay will send transactions to the
				//   coordinator starting from the next block only.
				//   This side effect can be fixed if we couple the signature verifier manager and
				//   validator-committer-manager goroutines in the coordinator with the stream between the sidecar
				//   and the coordinator. Thus, we can create input-output channels within the coordinator at the
				//   stream level to avoid this behavior. However, implementing this solution is significantly
				//   more complex; hence, we have opted for this simpler approach.
				// - Case 2: Block not match.
				//   Assume the same scenario described above. The only difference is that we find the newly
				//   enqueued txID is a duplicate of a previously submitted txID. In such a case, the block
				//   number in the txStatus does not match the block number being tracked by the relay for
				//   the same txID.
				continue
			}

			blkWithStatus, blkOK := r.blkNumToBlkWithStatus.Load(txStatus.BlockNumber)
			if !blkOK {
				// This can never occur unless there is a bug in the relay.
				return errors.Newf("block %d has never been submitted", txStatus.BlockNumber)
			}
			txNum, txMapOK := blkWithStatus.txIDToTxNum[txID]
			if !txMapOK {
				// This can never occur unless there is a bug in the relay.
				return errors.Newf("no transaction number for txID [%s]", txID)
			}
			if blkWithStatus.txStatus[txNum] != validationCode(statusNotYetValidated) {
				// This can never occur unless there is a bug in the relay or the coordinator.
				return errors.Newf("two results for the same TX (txID=%v). blockNum: %d, txNum: %d",
					txID, txStatus.BlockNumber, txNum)
			}

			blkWithStatus.txStatus[txNum] = byte(txStatus.Code)
			promutil.AddToCounterVec(r.metrics.transactionsStatusReceivedTotal,
				[]string{txStatus.GetCode().String()}, 1)

			r.txIDToBlkNum.Delete(txID)
			blkWithStatus.pendingCount--
			txStatusProcessedCount++
		}

		r.waitingTxsSlots.Release(txStatusProcessedCount)
		promutil.AddToGauge(r.metrics.waitingTransactionsQueueSize, -int(txStatusProcessedCount))
		r.processCommittedBlocksInOrder(ctx, outgoingCommittedBlock)
		promutil.Observe(r.metrics.transactionStatusesProcessingInRelaySeconds, time.Since(startTime))
	}
}

func (r *relay) processCommittedBlocksInOrder(
	ctx context.Context,
	outgoingCommittedBlock channel.Writer[*common.Block],
) {
	for ctx.Err() == nil {
		nextBlockNumberToBeCommitted := r.nextBlockNumberToBeCommitted.Load()
		blkWithStatus, exists := r.blkNumToBlkWithStatus.Load(nextBlockNumberToBeCommitted)
		if !exists {
			logger.Debugf("Next block [%d] to be committed is not in progress", nextBlockNumberToBeCommitted)
			return
		}
		if blkWithStatus.pendingCount > 0 {
			return
		}
		logger.Debugf("Next block [%d] has been committed", nextBlockNumberToBeCommitted)

		r.blkNumToBlkWithStatus.Delete(nextBlockNumberToBeCommitted)
		r.nextBlockNumberToBeCommitted.Add(1)
		r.activeBlocksCount.Add(-1)

		blkWithStatus.block.Metadata = &common.BlockMetadata{
			Metadata: [][]byte{nil, nil, blkWithStatus.txStatus},
		}
		outgoingCommittedBlock.Write(blkWithStatus.block)
	}
}

func (r *relay) setLastCommittedBlockNumber(
	ctx context.Context,
	client protocoordinatorservice.CoordinatorClient,
	expectedNextBlockToBeCommitted uint64,
) error {
	for {
		// NOTE: We are not strictly committing each committed block
		//       number immediately and also not in sequence.
		//       Instead, there is an implicit batching of block number.
		//       Even if the last committed block number
		//       set in the committer is different from the actual last committed
		//       block number, we have adequate recovery mechanism to detect
		//       them and recover correctly after a failure.

		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "context ended")
		case <-time.After(r.lastCommittedBlockSetInterval):
		}

		if r.nextBlockNumberToBeCommitted.Load() == expectedNextBlockToBeCommitted {
			continue
		}

		blkNum := r.nextBlockNumberToBeCommitted.Load() - 1
		logger.Debugf("Setting the last committed block number: %d", blkNum)
		_, err := client.SetLastCommittedBlockNumber(ctx, &protoblocktx.BlockInfo{Number: blkNum})
		if err != nil {
			return errors.Wrapf(err, "failed to set the last committed block number [%d]", blkNum)
		}
		expectedNextBlockToBeCommitted = blkNum + 1
	}
}
