package sidecar

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
)

type blockNumber = uint64

type txStatusAggregator struct {
	nextBlock        blockNumber
	inProgressBlocks map[blockNumber]*inProgressBlock
	completedBlocks  chan *common.Block
	m                sync.RWMutex
}
type inProgressBlock struct {
	block     *common.Block
	returned  []coordinatorservice.Status
	remaining int
}

func NewTxStatusAggregator() *txStatusAggregator {
	return &txStatusAggregator{
		inProgressBlocks: make(map[blockNumber]*inProgressBlock, 100),
		completedBlocks:  make(chan *common.Block, 10),
	}
}

func (a *txStatusAggregator) AddSubmittedBlock(block *common.Block, expected int) {
	logger.Infof("Adding block %d with %d non-config, non-issuing TXs to the aggregator.", block.Header.Number, expected)
	newBlock := &inProgressBlock{
		block:     block,
		returned:  make([]coordinatorservice.Status, expected),
		remaining: expected,
	}

	currentBlockNum := newBlock.block.Header.Number
	nextBlockNum := atomic.LoadUint64(&a.nextBlock)
	if currentBlockNum < nextBlockNum {
		panic(fmt.Sprintf("block %d already committed and output (until block %d)", currentBlockNum, nextBlockNum))
	}

	a.m.Lock()
	a.inProgressBlocks[currentBlockNum] = newBlock
	a.m.Unlock()

	a.tryCompleteBlock(newBlock)
}

func (a *txStatusAggregator) AddCommittedBatch(batch *coordinatorservice.TxValidationStatusBatch) {
	logger.Infof("Adding commited batch with %d TXs to the aggregator", len(batch.TxsValidationStatus))
	logger.Debugf("Batch: %v", batch)
	// We aggregate by block number to reduce the required accesses to the shared map a.inProgressBlocks
	statusByBlockNumber := make(map[blockNumber][]*coordinatorservice.TxValidationStatus, 50)
	for _, txStatus := range batch.TxsValidationStatus {
		txStatuses, ok := statusByBlockNumber[txStatus.BlockNum]
		if !ok {
			txStatuses = make([]*coordinatorservice.TxValidationStatus, 0, 20)
		}
		statusByBlockNumber[txStatus.BlockNum] = append(txStatuses, txStatus)
	}
	for blockNum, txStatuses := range statusByBlockNumber {

		a.m.Lock()
		currentBlock, ok := a.inProgressBlocks[blockNum]
		a.m.Unlock()

		if !ok {
			panic(fmt.Sprintf("block %d has never been submitted", blockNum))
		}
		for _, txStatus := range txStatuses {
			if currentBlock.returned[txStatus.TxNum] != coordinatorservice.Status_UNKNOWN {
				panic(fmt.Sprintf("two results for the same TX. blockNum: %d, txNum: %d", blockNum, txStatus.TxNum))
			}
			currentBlock.returned[txStatus.TxNum] = txStatus.Status
			// TODO: Possibly modify the block here, if the status is invalid
		}
		currentBlock.remaining -= len(txStatuses)
		a.tryCompleteBlock(currentBlock)
	}
}

func (a *txStatusAggregator) tryCompleteBlock(currentBlock *inProgressBlock) {
	blockNum := currentBlock.block.Header.Number
	if currentBlock.remaining > 0 {
		return
	}

	// When we start listening, we will start from the frist block that arrives
	if !atomic.CompareAndSwapUint64(&a.nextBlock, 0, blockNum+1) && !atomic.CompareAndSwapUint64(&a.nextBlock, blockNum, blockNum+1) {
		logger.Infof("Completed block %d, but block %d must be completed first (Remaining %d TXs).", blockNum, a.nextBlock, a.inProgressBlocks[a.nextBlock].remaining)
		return
	}

	a.m.Lock()
	delete(a.inProgressBlocks, blockNum)
	nextBlock, ok := a.inProgressBlocks[blockNum+1]
	a.m.Unlock()

	a.completedBlocks <- currentBlock.block
	if ok {
		a.tryCompleteBlock(nextBlock)
	}
}

func (a *txStatusAggregator) RunCommittedBlockListener(onFullBlockStatusComplete func(*common.Block)) {
	logger.Infof("Starting listener to committed blocks.\n")
	for {
		onFullBlockStatusComplete(<-a.completedBlocks)
	}
}
