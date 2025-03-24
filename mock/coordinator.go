package mock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

// Coordinator is a mock coordinator.
type Coordinator struct {
	protocoordinatorservice.CoordinatorServer
	lastCommittedBlockNumber atomic.Int64
	nextExpectedBlockNumber  atomic.Uint64
	streamActive             *sync.Mutex
	stop                     chan any
	numWaitingTxs            atomic.Int32

	txsStatus   map[string]*protoblocktx.StatusWithHeight
	txsStatusMu *sync.Mutex
}

// We don't want to utilize unlimited memory for storing the transactions status.
// A value of 1000 is adequate for most of the unit-test.
var defaultTxStatusStorageCleanupBlockInterval = 1000

// NewMockCoordinator creates a new mock coordinator.
func NewMockCoordinator() *Coordinator {
	c := &Coordinator{
		streamActive: &sync.Mutex{},
		stop:         make(chan any),
		txsStatus:    make(map[string]*protoblocktx.StatusWithHeight),
		txsStatusMu:  &sync.Mutex{},
	}
	c.lastCommittedBlockNumber.Store(-1)
	return c
}

// Close closes the mock coordinator.
func (c *Coordinator) Close() {
	logger.Infof("Closing mock coordinator")
	close(c.stop)
}

// GetConfigTransaction implements [protocoordinatorservice.GetConfigTransaction].
func (*Coordinator) GetConfigTransaction(
	context.Context, *protocoordinatorservice.Empty,
) (*protoblocktx.ConfigTransaction, error) {
	return nil, nil
}

// SetLastCommittedBlockNumber sets the last committed block number.
func (c *Coordinator) SetLastCommittedBlockNumber(
	_ context.Context, lastBlock *protoblocktx.BlockInfo,
) (*protocoordinatorservice.Empty, error) {
	c.lastCommittedBlockNumber.Store(int64(lastBlock.Number)) // nolint:gosec
	return &protocoordinatorservice.Empty{}, nil
}

// GetLastCommittedBlockNumber returns the last committed block number.
func (c *Coordinator) GetLastCommittedBlockNumber(
	_ context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	return &protoblocktx.BlockInfo{Number: uint64(c.lastCommittedBlockNumber.Load())}, nil // nolint:gosec
}

// GetNextExpectedBlockNumber returns the next expected block number to be received by the coordinator.
func (c *Coordinator) GetNextExpectedBlockNumber(
	_ context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	return &protoblocktx.BlockInfo{Number: c.nextExpectedBlockNumber.Load()}, nil
}

// GetTransactionsStatus returns the status of given set of transaction identifiers.
func (c *Coordinator) GetTransactionsStatus(
	_ context.Context,
	q *protoblocktx.QueryStatus,
) (*protoblocktx.TransactionsStatus, error) {
	status := make(map[string]*protoblocktx.StatusWithHeight)

	c.txsStatusMu.Lock()
	defer c.txsStatusMu.Unlock()
	for _, txID := range q.TxIDs {
		status[txID] = c.txsStatus[txID]
	}

	return &protoblocktx.TransactionsStatus{Status: status}, nil
}

// NumberOfWaitingTransactionsForStatus returns the number of transactions waiting to get the final status.
func (c *Coordinator) NumberOfWaitingTransactionsForStatus(
	_ context.Context,
	_ *protocoordinatorservice.Empty,
) (*protocoordinatorservice.WaitingTransactions, error) {
	return &protocoordinatorservice.WaitingTransactions{
		Count: c.numWaitingTxs.Load(),
	}, nil
}

// BlockProcessing processes a block.
func (c *Coordinator) BlockProcessing(stream protocoordinatorservice.Coordinator_BlockProcessingServer) error {
	if !c.streamActive.TryLock() {
		return errors.New("stream is already active. Only one stream is allowed")
	}
	defer c.streamActive.Unlock()

	input := make(chan *protocoordinatorservice.Block, 1000)
	defer close(input)
	defer logger.Infof("Closed mock coordinator")

	go c.sendTxsValidationStatus(stream, input)

	// start listening
	for {
		select {
		case <-c.stop:
			logger.Infof("Stopping server")
			return nil
		default:
		}
		block, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if block.Number != c.nextExpectedBlockNumber.Load() {
			return fmt.Errorf(
				"the received block [%d] is different from the expected block [%d]",
				block.Number,
				c.nextExpectedBlockNumber.Load(),
			)
		}
		c.nextExpectedBlockNumber.Add(1)

		if c.nextExpectedBlockNumber.Load()%uint64(defaultTxStatusStorageCleanupBlockInterval) == 0 { // nolint:gosec
			c.txsStatusMu.Lock()
			c.txsStatus = make(map[string]*protoblocktx.StatusWithHeight)
			c.txsStatusMu.Unlock()
		}

		if len(block.Txs) != len(block.TxsNum) {
			return fmt.Errorf("the block doesn't have the correct number of transactions set")
		}

		logger.Debugf("Received block %d with %d transactions", block.Number, len(block.Txs))
		c.numWaitingTxs.Add(int32(len(block.Txs))) // nolint:gosec

		// send to the validation
		select {
		case <-c.stop:
			logger.Infof("Stopping server")
			return nil
		case input <- block:
		}
	}
}

// IsStreamActive returns true if the stream from the sidecar is active.
func (c *Coordinator) IsStreamActive() bool {
	if c.streamActive.TryLock() {
		defer c.streamActive.Unlock()
		return false
	}
	return true
}

func (c *Coordinator) sendTxsValidationStatus(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
	input chan *protocoordinatorservice.Block,
) {
	for scBlock := range input {
		txs := scBlock.Txs
		txCount := 0
		c.txsStatusMu.Lock()
		for len(txs) > 0 {
			chunkSize := rand.Intn(len(txs)) + 1
			b := &protoblocktx.TransactionsStatus{Status: make(map[string]*protoblocktx.StatusWithHeight)}
			for _, tx := range txs[:chunkSize] {
				s := types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, scBlock.Number,
					int(scBlock.TxsNum[txCount]))
				b.Status[tx.GetId()] = s
				c.txsStatus[tx.GetId()] = s
				txCount++
			}

			if rpcErr := stream.Send(b); connection.IsStreamEnd(rpcErr) {
				logger.Debugf("stream ended")
			} else {
				utils.Must(connection.FilterStreamRPCError(rpcErr))
			}
			logger.Debugf("Sent back batch with %d TXs", len(b.Status))
			c.numWaitingTxs.Add(-int32(len(b.Status))) // nolint:gosec

			txs = txs[chunkSize:]
		}
		c.txsStatusMu.Unlock()
	}
}

// SetWaitingTxsCount sets the waiting transactions count. The purpose
// of this method is to set the count manually for testing purpose.
func (c *Coordinator) SetWaitingTxsCount(count int32) {
	c.numWaitingTxs.Store(count)
}
