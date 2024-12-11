package sidecar

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"golang.org/x/sync/errgroup"
)

type (
	relay struct {
		coordConfig                   *CoordinatorConfig
		incomingBlockToBeCommitted    <-chan *common.Block
		outgoingCommittedBlock        chan<- *common.Block
		nextBlockNumberToBeCommitted  atomic.Uint64
		activeBlocksCount             atomic.Int32
		blkNumToBlkWithStatus         sync.Map
		txIDToBlkNum                  sync.Map
		lastCommittedBlockSetInterval time.Duration
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
	}
)

func newRelay(
	config *CoordinatorConfig,
	uncommittedBlock <-chan *common.Block,
	committedBlock chan<- *common.Block,
) *relay {
	return &relay{
		coordConfig:                   config,
		incomingBlockToBeCommitted:    uncommittedBlock,
		outgoingCommittedBlock:        committedBlock,
		lastCommittedBlockSetInterval: 5 * time.Second, // TODO: make it configurable.
	}
}

// Run starts the relay service. The call to Run blocks until an error occurs or the context is canceled.
func (r *relay) Run(ctx context.Context, config *relayRunConfig) error {
	r.nextBlockNumberToBeCommitted.Store(config.nextExpectedBlockByCoordinator)

	rCtx, rCancel := context.WithCancel(ctx)
	defer rCancel()

	stream, err := config.coordClient.BlockProcessing(rCtx)
	if err != nil {
		return fmt.Errorf("failed to open stream for block processing: %w", err)
	}

	logger.Infof("Starting coordinator sender and receiver")

	expectedNextBlockToBeCommitted := r.nextBlockNumberToBeCommitted.Load()

	g, gCtx := errgroup.WithContext(stream.Context())
	g.Go(func() error {
		return r.preProcessBlockAndSendToCoordinator(gCtx, stream)
	})

	statusBatch := make(chan *protocoordinatorservice.TxValidationStatusBatch, 1000)
	g.Go(func() error {
		return receiveStatusFromCoordinator(gCtx, stream, statusBatch)
	})

	g.Go(func() error {
		return r.processStatusBatch(gCtx, statusBatch)
	})

	g.Go(func() error {
		return r.setLastCommittedBlockNumber(gCtx, config.coordClient, expectedNextBlockToBeCommitted)
	})

	return g.Wait()
}

func (r *relay) preProcessBlockAndSendToCoordinator( // nolint:gocognit
	ctx context.Context,
	stream protocoordinatorservice.Coordinator_BlockProcessingClient,
) error {
	incomingBlockToBeCommitted := channel.NewReader(ctx, r.incomingBlockToBeCommitted)
	outgoingCommittedBlock := channel.NewWriter(ctx, r.outgoingCommittedBlock)
	for {
		block, ok := incomingBlockToBeCommitted.Read()
		if !ok {
			return nil
		}
		blockNum := block.Header.Number
		logger.Debugf("Block %d arrived in the relay", blockNum)

		// TODO: If we are expecting only scalable committer transactions, we
		//       do not need to filter any transactions. We need to discuss about
		//       the potential use-case. If there is none, we can remove the
		//       filtering of transactions.
		scBlock, filteredTxsIndex := mapBlock(block)

		txCount := len(block.Data.Data)
		blkWithResult := &blockWithStatus{
			block:         block,
			txStatus:      newValidationCodes(txCount),
			txIDToTxIndex: make(map[string]int, txCount),
			pendingCount:  txCount - len(filteredTxsIndex),
		}

		// set all filtered transaction to valid by default
		// TODO once SC V2 can process config transaction and alike, this needs to be changed
		for txIndex := range filteredTxsIndex {
			logger.Debugf("TX [%d:%d] is excluded: %v", blockNum, txIndex, excludedStatus)
			blkWithResult.txStatus[txIndex] = excludedStatus
		}

		r.blkNumToBlkWithStatus.Store(blockNum, blkWithResult)

		dupIdx := make([]int, 0, len(scBlock.Txs))
		for txIndex, tx := range scBlock.Txs {
			if _, loaded := r.txIDToBlkNum.LoadOrStore(tx.GetId(), blockNum); !loaded {
				logger.Debugf("Adding txID [%s] to in progress list", tx.GetId())
				blkWithResult.txIDToTxIndex[tx.GetId()] = txIndex
				continue
			}
			logger.Debugf("txID [%s] is duplicate", tx.GetId())
			blkWithResult.pendingCount--
			blkWithResult.txStatus[txIndex] = byte(protoblocktx.Status_ABORTED_DUPLICATE_TXID)
			dupIdx = append(dupIdx, txIndex)
		}

		// Iterate over the indices in reverse order. Note that the dupIdx is sorted by default.
		for _, index := range slices.Backward(dupIdx) {
			scBlock.Txs = append(scBlock.Txs[:index], scBlock.Txs[index+1:]...)
			scBlock.TxsNum = append(scBlock.TxsNum[:index], scBlock.TxsNum[index+1:]...)
		}

		r.activeBlocksCount.Add(1)

		// TODO: The following needs to be validated when the config transaction
		//       is implemented.
		//       In the case that we got a config block, we will not get any TX status
		//       back from the coordinator, so we register it here as completed.
		//       We also send it to the coordinator, so that it keeps track of which
		//       blocks have already passed (to avoid delivering out of order blocks)
		if len(scBlock.GetTxs()) == 0 && blkWithResult.pendingCount == 0 {
			r.processCommittedBlocksInOrder(ctx, outgoingCommittedBlock)
		}

		if err := stream.SendMsg(scBlock); err != nil {
			return connection.FilterStreamErrors(err)
		}
		logger.Debugf("Sent scBlock %d with %d transactions to Coordinator", scBlock.Number, len(scBlock.Txs))
	}
}

func receiveStatusFromCoordinator(
	ctx context.Context,
	stream protocoordinatorservice.Coordinator_BlockProcessingClient,
	statusBatch chan<- *protocoordinatorservice.TxValidationStatusBatch,
) error {
	txsStatus := channel.NewWriter(ctx, statusBatch)
	for {
		response, err := stream.Recv()
		if err != nil {
			return err
		}
		logger.Debugf("Received status batch (%d updates) from coordinator", len(response.GetTxsValidationStatus()))

		txsStatus.Write(response)
	}
}

func (r *relay) processStatusBatch(
	ctx context.Context,
	statusBatch <-chan *protocoordinatorservice.TxValidationStatusBatch,
) error {
	txsStatus := channel.NewReader(ctx, statusBatch)
	outgoingCommittedBlock := channel.NewWriter(ctx, r.outgoingCommittedBlock)
	for {
		tStatus, ok := txsStatus.Read()
		if !ok {
			return nil
		}

		for _, txStatus := range tStatus.GetTxsValidationStatus() {
			txID := txStatus.GetTxId()
			blockNum, ok := r.txIDToBlkNum.Load(txID)
			if !ok {
				return fmt.Errorf("TxID = %v is not associated with a block", txID)
			}

			v, ok := r.blkNumToBlkWithStatus.Load(blockNum)
			if !ok {
				return fmt.Errorf("block %d has never been submitted", blockNum)
			}
			blkWithStatus, _ := v.(*blockWithStatus) // nolint:revive

			txIndex := blkWithStatus.txIDToTxIndex[txID]

			if blkWithStatus.txStatus[txIndex] != notYetValidated {
				return fmt.Errorf("two results for the same TX (txID=%v). blockNum: %d, txNum: %d",
					txID, blockNum, txIndex)
			}

			blkWithStatus.txStatus[txIndex] = byte(txStatus.GetStatus())

			r.txIDToBlkNum.Delete(txID)
			blkWithStatus.pendingCount--
		}

		r.processCommittedBlocksInOrder(ctx, outgoingCommittedBlock)
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
			return err
		}
		expectedNextBlockToBeCommitted = blkNum + 1
	}
}
