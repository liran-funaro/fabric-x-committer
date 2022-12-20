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

func (a *txStatusAggregator) AddSubmittedConfigBlock(block *common.Block) {
	a.addSubmittedBlock(&inProgressBlock{
		block:     block,
		returned:  make([]coordinatorservice.Status, 0),
		remaining: 0,
	})
}

func (a *txStatusAggregator) AddSubmittedTxBlock(block *common.Block) {
	a.addSubmittedBlock(&inProgressBlock{
		block:     block,
		returned:  make([]coordinatorservice.Status, len(block.Data.Data)), // All elements are by default 0 = UNKNOWN
		remaining: len(block.Data.Data),
	})
}

func (a *txStatusAggregator) addSubmittedBlock(newBlock *inProgressBlock) {
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

	if !atomic.CompareAndSwapUint64(&a.nextBlock, blockNum, blockNum+1) {
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
	for {
		onFullBlockStatusComplete(<-a.completedBlocks)
	}
}
