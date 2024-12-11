package mock

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

// Coordinator is a mock coordinator.
type Coordinator struct {
	protocoordinatorservice.CoordinatorServer
	lastCommittedBlockNumber atomic.Int64
	nextExpectedBlockNumber  atomic.Uint64
	stop                     chan any
}

// NewMockCoordinator creates a new mock coordinator.
func NewMockCoordinator() *Coordinator {
	c := &Coordinator{
		stop: make(chan any),
	}
	c.lastCommittedBlockNumber.Store(-1)
	return c
}

// Close closes the mock coordinator.
func (c *Coordinator) Close() {
	logger.Infof("Closing mock coordinator")
	close(c.stop)
}

// SetMetaNamespaceVerificationKey sets the verification key.
func (*Coordinator) SetMetaNamespaceVerificationKey(
	_ context.Context, _ *protosigverifierservice.Key,
) (*protocoordinatorservice.Empty, error) {
	return &protocoordinatorservice.Empty{}, nil
}

// SetLastCommittedBlockNumber sets the last committed block number.
func (c *Coordinator) SetLastCommittedBlockNumber(
	_ context.Context, lastBlock *protoblocktx.BlockInfo,
) (*protocoordinatorservice.Empty, error) {
	c.lastCommittedBlockNumber.Store(int64(lastBlock.Number))
	return &protocoordinatorservice.Empty{}, nil
}

// GetLastCommittedBlockNumber returns the last committed block number.
func (c *Coordinator) GetLastCommittedBlockNumber(
	_ context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	return &protoblocktx.BlockInfo{Number: uint64(c.lastCommittedBlockNumber.Load())}, nil
}

// GetNextExpectedBlockNumber returns the next expected block number to be received by the coordinator.
func (c *Coordinator) GetNextExpectedBlockNumber(
	_ context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	return &protoblocktx.BlockInfo{Number: c.nextExpectedBlockNumber.Load()}, nil
}

// BlockProcessing processes a block.
func (c *Coordinator) BlockProcessing(stream protocoordinatorservice.Coordinator_BlockProcessingServer) error {
	input := make(chan *protoblocktx.Block, 1000)
	defer close(input)
	defer logger.Infof("Closed mock coordinator")

	go sendTxsValidationStatus(stream, input)

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

		if len(block.Txs) != len(block.TxsNum) {
			return fmt.Errorf("the block doesn't have the correct number of transactions set")
		}

		logger.Debugf("Received block %d with %d transactions", block.Number, len(block.Txs))

		// send to the validation
		select {
		case <-c.stop:
			logger.Infof("Stopping server")
			return nil
		case input <- block:
		}
	}
}

func sendTxsValidationStatus(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
	input chan *protoblocktx.Block,
) {
	for scBlock := range input {
		batch := &protocoordinatorservice.TxValidationStatusBatch{
			TxsValidationStatus: make([]*protocoordinatorservice.TxValidationStatus, len(scBlock.GetTxs())),
		}

		for i, tx := range scBlock.GetTxs() {
			batch.TxsValidationStatus[i] = &protocoordinatorservice.TxValidationStatus{
				TxId:   tx.GetId(),
				Status: protoblocktx.Status_COMMITTED,
			}
		}

		// coordinator sends responses in multiple chunks (parts)
		for len(batch.TxsValidationStatus) > 0 {
			chunkSize := rand.Intn(len(batch.TxsValidationStatus)) + 1
			chunk := batch.TxsValidationStatus[:chunkSize]
			batch.TxsValidationStatus = batch.TxsValidationStatus[chunkSize:]

			rpcErr := stream.Send(&protocoordinatorservice.TxValidationStatusBatch{TxsValidationStatus: chunk})
			if connection.IsStreamEnd(rpcErr) {
				logger.Debugf("stream ended")
			} else {
				utils.Must(connection.WrapStreamRpcError(rpcErr))
			}
			logger.Debugf("Sent back batch with %d TXs", len(chunk))
		}
	}
}
