package sidecar

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"golang.org/x/sync/errgroup"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type (
	relay struct {
		incomingBlockToBeCommitted    <-chan *common.Block
		outgoingCommittedBlock        chan<- *common.Block
		nextBlockNumberToBeCommitted  atomic.Uint64
		activeBlocksCount             atomic.Int32
		blkNumToBlkWithStatus         sync.Map
		txIDToBlkNum                  sync.Map
		lastCommittedBlockSetInterval time.Duration
		metrics                       *perfMetrics
	}

	blockWithStatus struct {
		block         *common.Block
		txStatus      []validationCode
		txIDToTxIndex map[string]int
		pendingCount  int
	}

	relayRunConfig struct {
		coordClient                    protocoordinatorservice.CoordinatorClient
		nextExpectedBlockByCoordinator uint64
		configUpdater                  func(*common.Block)
	}
)

const defaultLastCommittedBlockSetInterval = 5 * time.Second

func newRelay(
	incomingBlockToBeCommitted <-chan *common.Block,
	committedBlock chan<- *common.Block,
	lastCommittedBlockSetInterval time.Duration,
	metrics *perfMetrics,
) *relay {
	if lastCommittedBlockSetInterval == 0 {
		lastCommittedBlockSetInterval = defaultLastCommittedBlockSetInterval
	}
	return &relay{
		incomingBlockToBeCommitted:    incomingBlockToBeCommitted,
		outgoingCommittedBlock:        committedBlock,
		lastCommittedBlockSetInterval: lastCommittedBlockSetInterval,
		metrics:                       metrics,
	}
}

// Run starts the relay service. The call to Run blocks until an error occurs or the context is canceled.
func (r *relay) Run(ctx context.Context, config *relayRunConfig) error {
	r.nextBlockNumberToBeCommitted.Store(config.nextExpectedBlockByCoordinator)

	rCtx, rCancel := context.WithCancel(ctx)
	defer rCancel()

	stream, err := config.coordClient.BlockProcessing(rCtx)
	if err != nil {
		return errors.Wrap(err, "failed to open stream for block processing")
	}

	logger.Infof("Starting coordinator sender and receiver")

	expectedNextBlockToBeCommitted := r.nextBlockNumberToBeCommitted.Load()

	g, gCtx := errgroup.WithContext(stream.Context())
	g.Go(func() error {
		return r.preProcessBlockAndSendToCoordinator(gCtx, stream, config.configUpdater)
	})

	statusBatch := make(chan *protoblocktx.TransactionsStatus, 1000)
	g.Go(func() error {
		return receiveStatusFromCoordinator(gCtx, stream, statusBatch)
	})

	g.Go(func() error {
		return r.processStatusBatch(gCtx, statusBatch)
	})

	g.Go(func() error {
		return r.setLastCommittedBlockNumber(gCtx, config.coordClient, expectedNextBlockToBeCommitted)
	})

	return errors.Wrap(g.Wait(), "stream with the coordinator has ended")
}

func (r *relay) preProcessBlockAndSendToCoordinator( //nolint:gocognit
	ctx context.Context,
	stream protocoordinatorservice.Coordinator_BlockProcessingClient,
	configUpdater func(*common.Block),
) error {
	incomingBlockToBeCommitted := channel.NewReader(ctx, r.incomingBlockToBeCommitted)
	outgoingCommittedBlock := channel.NewWriter(ctx, r.outgoingCommittedBlock)
	for {
		block, ok := incomingBlockToBeCommitted.Read()
		if !ok {
			return nil
		}
		if block.Header == nil {
			logger.Warn("Received a block without header")
			continue
		}

		startTime := time.Now()
		blockNum := block.Header.Number
		logger.Debugf("Block %d arrived in the relay", blockNum)

		mappedBlock := mapBlock(block)
		if mappedBlock.isConfig {
			configUpdater(block)
		}

		r.blkNumToBlkWithStatus.Store(blockNum, mappedBlock.withStatus)

		dupIdx := make([]int, 0, len(mappedBlock.block.Txs))
		for txIndex, tx := range mappedBlock.block.Txs {
			if _, loaded := r.txIDToBlkNum.LoadOrStore(tx.Id, blockNum); !loaded {
				logger.Debugf("Adding txID [%s] to in progress list", tx.GetId())
				mappedBlock.withStatus.txIDToTxIndex[tx.Id] = txIndex
				continue
			}
			logger.Debugf("txID [%s] is duplicate", tx.GetId())
			monitoring.AddToCounterVec(r.metrics.transactionsStatusReceivedTotal, []string{
				protoblocktx.Status_ABORTED_DUPLICATE_TXID.String(),
			}, 1)
			mappedBlock.withStatus.pendingCount--
			mappedBlock.withStatus.txStatus[txIndex] = validationCode(protoblocktx.Status_ABORTED_DUPLICATE_TXID)
			dupIdx = append(dupIdx, txIndex)
		}

		// Iterate over the indices in reverse order. Note that the dupIdx is sorted by default.
		for _, index := range slices.Backward(dupIdx) {
			mappedBlock.block.Txs = append(mappedBlock.block.Txs[:index], mappedBlock.block.Txs[index+1:]...)
			mappedBlock.block.TxsNum = append(mappedBlock.block.TxsNum[:index], mappedBlock.block.TxsNum[index+1:]...)
		}

		r.activeBlocksCount.Add(1)

		txsCount := len(mappedBlock.block.Txs)
		if txsCount == 0 && mappedBlock.withStatus.pendingCount == 0 {
			r.processCommittedBlocksInOrder(ctx, outgoingCommittedBlock)
		}

		if err := stream.Send(mappedBlock.block); err != nil {
			return errors.Wrap(err, "failed to send a block to the coordinator")
		}
		monitoring.AddToCounter(r.metrics.transactionsSentTotal, txsCount)
		logger.Debugf("Sent SC block %d with %d transactions to Coordinator",
			mappedBlock.block.Number, txsCount)
		monitoring.Observe(r.metrics.blockProcessingInRelaySeconds, time.Since(startTime))
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
		tStatus, ok := txsStatus.Read()
		if !ok {
			return nil
		}

		startTime := time.Now()
		for txID, txStatus := range tStatus.GetStatus() {
			blockNum, ok := r.txIDToBlkNum.Load(txID)
			if !ok {
				return fmt.Errorf("TxID = %v is not associated with a block", txID)
			}

			v, ok := r.blkNumToBlkWithStatus.Load(blockNum)
			if !ok {
				return fmt.Errorf("block %d has never been submitted", blockNum)
			}
			blkWithStatus, _ := v.(*blockWithStatus) //nolint:revive

			txIndex := blkWithStatus.txIDToTxIndex[txID]

			if blkWithStatus.txStatus[txIndex] != notYetValidated {
				return fmt.Errorf("two results for the same TX (txID=%v). blockNum: %d, txNum: %d",
					txID, blockNum, txIndex)
			}

			blkWithStatus.txStatus[txIndex] = byte(txStatus.GetCode())
			monitoring.AddToCounterVec(r.metrics.transactionsStatusReceivedTotal,
				[]string{txStatus.GetCode().String()}, 1)

			r.txIDToBlkNum.Delete(txID)
			blkWithStatus.pendingCount--
		}

		r.processCommittedBlocksInOrder(ctx, outgoingCommittedBlock)
		monitoring.Observe(r.metrics.transactionStatusesProcessingInRelaySeconds, time.Since(startTime))
	}
}

func (r *relay) processCommittedBlocksInOrder(
	ctx context.Context,
	outgoingCommittedBlock channel.Writer[*common.Block],
) {
	for ctx.Err() == nil {
		nextBlockNumberToBeCommitted := r.nextBlockNumberToBeCommitted.Load()
		v, exists := r.blkNumToBlkWithStatus.Load(nextBlockNumberToBeCommitted)
		if !exists {
			logger.Debugf("Next block [%d] to be committed is not in progress", nextBlockNumberToBeCommitted)
			return
		}
		blkWithStatus, _ := v.(*blockWithStatus) //nolint:revive
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
			return nil
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
