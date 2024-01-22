package aggregator

import (
	"context"
	"fmt"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("aggregator")

type inProgressBlock struct {
	block     *common.Block
	returned  []validationCode
	txIDs     map[string]int
	remaining int
}

type Aggregator struct {
	// state
	nextCommittedBlockNum uint64
	enqueuedBlocks        uint64
	inProgressBlocks      map[uint64]*inProgressBlock
	inProgressTxs         map[string]uint64

	// adapter functions
	sendToCoordinator func(scBlock *protoblocktx.Block)
	output            func(block *common.Block)
	stop              chan any
}

func New(sendToCoordinator func(scBlock *protoblocktx.Block), output func(block *common.Block)) *Aggregator {
	return &Aggregator{
		nextCommittedBlockNum: 0,
		inProgressBlocks:      make(map[uint64]*inProgressBlock, 100),
		inProgressTxs:         make(map[string]uint64, 100000),
		sendToCoordinator:     sendToCoordinator,
		output:                output,
		stop:                  make(chan any),
	}
}

func (a *Aggregator) Close() {
	logger.Infof("Stop aggregator")
	close(a.stop)
}

func (a *Aggregator) Start(ctx context.Context, blockChan <-chan *common.Block, statusChan <-chan *protocoordinatorservice.TxValidationStatusBatch) chan error {
	errChan := make(chan error)
	go func() {
		errChan <- a.run(ctx, blockChan, statusChan)
	}()
	return errChan
}

func (a *Aggregator) run(ctx context.Context, blockChan <-chan *common.Block, statusChan <-chan *protocoordinatorservice.TxValidationStatusBatch) error {
	defer logger.Infof("Stopped running")
	logger.Infof("Started aggregator for sidecar")
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-a.stop:
			return nil
		case s, more := <-statusChan:
			//  1st priority: process status batches
			if err := a.processStatusBatch(s); err != nil {
				return errors.Wrap(err, "failed to process status batch")
			}
			if !more {
				return nil
			}
		default:
			select {
			case <-ctx.Done():
				return nil
			case <-a.stop:
				return nil
			case s, more := <-statusChan:
				if err := a.processStatusBatch(s); err != nil {
					return errors.Wrap(err, "failed to process status batch")
				}
				if !more {
					return nil
				}
			case b := <-blockChan:
				// 2nd priority: enqueue new blocks
				if err := a.enqueueNewBlock(b); err != nil {
					return errors.Wrap(err, "failed to enqueue new block")
				}
			}
		}
	}
}

func (a *Aggregator) enqueueNewBlock(block *common.Block) error {
	if block == nil || block.Data == nil || len(block.Data.Data) == 0 {
		logger.Debugf("Nil block enqueued")
		return nil
	}
	logger.Debugf("Block %d arrived in the aggregator", block.Header.Number)

	blockNum := block.Header.Number
	txCount := len(block.Data.Data)

	logger.Debugf("Enqueue new block %d", blockNum)

	if a.nextCommittedBlockNum > blockNum {
		panic(fmt.Sprintf("block %d already processed", blockNum))
	} else if a.enqueuedBlocks == 0 && blockNum != a.nextCommittedBlockNum {
		return fmt.Errorf("expected block num = %d, actual = %d", a.nextCommittedBlockNum, blockNum)
	}

	scBlock, filteredTxs := mapBlock(block)

	newBlock := &inProgressBlock{
		block:     block,
		returned:  newValidationCodes(txCount),
		txIDs:     make(map[string]int, txCount),
		remaining: txCount - len(filteredTxs),
	}
	logger.Debugf("New block [%d] has %d TX's of which %d were filtered", block.Header.Number, txCount, newBlock.remaining)

	// set all filtered transaction to valid by default
	// TODO once SC V2 can process config transaction and alike, this needs to be changed
	for pos := range filteredTxs {
		logger.Debugf("TX [%d:%d] is excluded: %v", blockNum, pos, excludedStatus)
		newBlock.returned[pos] = excludedStatus
	}

	a.inProgressBlocks[blockNum] = newBlock

	for i, tx := range scBlock.Txs {
		// create mapping of txID to position in block
		newBlock.txIDs[tx.GetId()] = i
		// keep track of block number and txID mapping
		a.inProgressTxs[tx.GetId()] = blockNum
	}

	a.enqueuedBlocks += 1

	// in the case that we got a config block, we will not get any TX status back from the coordinator, so we register it here as completed.
	// We also send it to the coordinator, so that it keeps track of which blocks have already passed (to avoid delivering out of order blocks)
	if len(scBlock.GetTxs()) == 0 && newBlock.remaining == 0 {
		a.tryComplete()
	}

	// submit block to coordinator
	a.sendToCoordinator(scBlock)
	return nil
}

func (a *Aggregator) processStatusBatch(batch *protocoordinatorservice.TxValidationStatusBatch) error {
	logger.Debugf("New status batch arrived in the aggregator")
	for _, txStatus := range batch.GetTxsValidationStatus() {
		blockNum, ok := a.inProgressTxs[txStatus.GetTxId()]
		if !ok {
			return fmt.Errorf("TxID = %v is not associated with a block", txStatus.GetTxId())
		}

		block, ok := a.inProgressBlocks[blockNum]
		if !ok {
			return fmt.Errorf("block %d has never been submitted", blockNum)
		}

		// check that txID is really part of the block
		pos, ok := block.txIDs[txStatus.GetTxId()]
		if !ok {
			return fmt.Errorf("transaction %v has never been submitted", txStatus.GetTxId())
		}

		// check that the tx has not yet been validated
		if block.returned[pos] != notYetValidated {
			return fmt.Errorf("two results for the same TX (txID=%v). blockNum: %d, txNum: %d", txStatus.GetTxId(), blockNum, pos)
		}

		block.returned[pos] = statusMap[txStatus.GetStatus()]
		logger.Debugf("Transaction [%d:%d] [%s] now has status: %v", blockNum, pos, txStatus.TxId, txStatus.Status)
		a.inProgressBlocks[blockNum] = block

		// cleanup
		delete(a.inProgressTxs, txStatus.GetTxId())
		block.remaining -= 1
	}

	// after each batch we try to complete the next block
	a.tryComplete()
	return nil
}

func (a *Aggregator) tryComplete() {
	currentBlockNum := a.nextCommittedBlockNum
	currentBlock, exists := a.inProgressBlocks[currentBlockNum]

	// seems we are not complete with this one ...
	if !exists || currentBlock.remaining > 0 {
		logger.Debugf("Next committed block [%d] is in progress [%v] or has no remaining TXs", currentBlockNum, exists)
		return
	}
	logger.Debugf("Next committed block [%d] is complete", currentBlockNum)

	// cleanup
	delete(a.inProgressBlocks, currentBlockNum)
	a.nextCommittedBlockNum += 1
	a.enqueuedBlocks -= 1

	// send block to output
	a.output(updateMetadata(currentBlock.block, currentBlock.returned))

	// try to complete as many blocks as we can (recursion)
	a.tryComplete()
}

func updateMetadata(block *common.Block, metadataTransactionFilter []byte) *common.Block {
	if block.Metadata == nil {
		block.Metadata = &common.BlockMetadata{Metadata: make([][]byte, common.BlockMetadataIndex_TRANSACTIONS_FILTER+1)}
	}
	block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER] = metadataTransactionFilter
	return block
}
