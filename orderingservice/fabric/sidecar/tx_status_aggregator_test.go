package sidecar_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

const defaultTimeout = 2 * time.Second

func TestConfigs(t *testing.T) {
	test.FailHandler(t)
	i := NewTestInstance()
	outputCh := i.StartOutputWriter()

	i.SubmitToOrderer(createBlock(0, 0), createBlock(1, 0), createBlock(2, 0))
	i.AssertReceivedBlocks(outputCh, 0, 2)
}

func TestBaseCase(t *testing.T) {
	test.FailHandler(t)
	i := NewTestInstance()
	outputCh := i.StartOutputWriter()

	i.SubmitToOrderer(createBlock(0, 0), createBlock(1, 3))
	i.ReturnFromCommitter(createValidStatus(1, 1),
		createValidStatus(1, 2),
		createValidStatus(1, 0),
	)
	i.AssertReceivedBlocks(outputCh, 0, 1)

	i.SubmitToOrderer(createBlock(2, 2), createBlock(3, 2))
	i.ReturnFromCommitter(createValidStatus(2, 0),
		createValidStatus(2, 1),
		createValidStatus(3, 0),
		createValidStatus(3, 1),
	)
	i.AssertReceivedBlocks(outputCh, 2, 3)
}

func TestBlockInReverseOrder(t *testing.T) {
	test.FailHandler(t)
	i := NewTestInstance()
	outputCh := i.StartOutputWriter()

	i.SubmitToOrderer(createBlock(0, 0), createBlock(1, 2), createBlock(2, 2))
	i.ReturnFromCommitter(createValidStatus(1, 1),
		createValidStatus(2, 0),
		createValidStatus(2, 1),
	)
	i.AssertReceivedBlocks(outputCh, 0, 0)

	i.ReturnFromCommitter(createValidStatus(1, 0))
	i.AssertReceivedBlocks(outputCh, 1, 2)
}

func TestParallel(t *testing.T) {
	test.FailHandler(t)
	i := NewTestInstance()
	outputCh := i.StartOutputWriter()

	go func() {
		i.SubmitToOrderer(createBlock(0, 0), createBlock(1, 2), createBlock(2, 2))
		i.ReturnFromCommitter(createValidStatus(1, 1),
			createValidStatus(2, 0),
			createValidStatus(2, 1),
		)
	}()
	go func() {
		i.SubmitToOrderer(createBlock(3, 2), createBlock(4, 2))
		i.ReturnFromCommitter(createValidStatus(4, 0),
			createValidStatus(4, 1),
			createValidStatus(3, 0),
			createValidStatus(3, 1),
		)
		i.ReturnFromCommitter(createValidStatus(1, 0))
	}()

	i.AssertReceivedBlocks(outputCh, 0, 4)
}

func createValidStatus(blockNum, txNum uint64) *coordinatorservice.TxValidationStatus {
	return &coordinatorservice.TxValidationStatus{BlockNum: blockNum, TxNum: txNum, Status: coordinatorservice.Status_VALID}
}

func createValidStatuses(blockNum, txNumFrom, txNumTo uint64) []*coordinatorservice.TxValidationStatus {
	totalElements := txNumTo - txNumFrom
	result := make([]*coordinatorservice.TxValidationStatus, totalElements)
	for i := uint64(0); i < totalElements; i++ {
		result[i] = createValidStatus(blockNum, txNumFrom+i)
	}
	return result
}

type testInstance struct {
	aggregator sidecar.PostCommitAggregator
}

func NewTestInstance() *testInstance {
	return &testInstance{
		aggregator: sidecar.NewTxStatusAggregator(),
	}
}

func (i *testInstance) SubmitToOrderer(blocks ...*common.Block) {
	for _, block := range blocks {
		i.aggregator.AddSubmittedBlock(block, len(block.Data.Data))
	}
}

func (i *testInstance) ReturnFromCommitter(statuses ...*coordinatorservice.TxValidationStatus) {
	i.aggregator.AddCommittedBatch(&coordinatorservice.TxValidationStatusBatch{TxsValidationStatus: statuses})
}

func (i *testInstance) AssertReceivedBlocks(outputCh <-chan *common.Block, from, to int) {
	for j := from; j <= to; j++ {
		gomega.Eventually(outputCh).WithTimeout(defaultTimeout).Should(gomega.Receive(blockWithNumber(j)))
	}
}

func (i *testInstance) StartOutputWriter() <-chan *common.Block {
	outputCh := make(chan *common.Block, 10)
	go i.aggregator.RunCommittedBlockListener(func(block *common.Block) {
		outputCh <- block
	})
	return outputCh
}
func (i *testInstance) StartOutputLogger() {
	go i.aggregator.RunCommittedBlockListener(func(block *common.Block) {
		fmt.Printf("Output block: %d\n", block.Header.Number)
	})
}
func (i *testInstance) StartEmptyOutputConsumer() {
	go i.aggregator.RunCommittedBlockListener(func(block *common.Block) {})
}

func blockWithNumber(blockNumber int) types.GomegaMatcher {
	return gomega.Satisfy(func(b *common.Block) bool {
		return int(b.Header.Number) == blockNumber
	})
}

func createBlock(number, blockSize uint64) *common.Block {
	return &common.Block{
		Header: &common.BlockHeader{Number: number},
		Data:   &common.BlockData{Data: make([][]byte, blockSize)},
	}
}
