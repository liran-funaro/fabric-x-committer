package coordinatormock

import (
	"context"
	"io"
	"math/rand"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

var logger = logging.New("mock-coordinator")

// MockCoordinator is a mock coordinator.
type MockCoordinator struct {
	protocoordinatorservice.CoordinatorServer
	lastCommittedBlockNumber atomic.Int64
	stop                     chan any
}

// NewMockCoordinator creates a new mock coordinator.
func NewMockCoordinator() *MockCoordinator {
	c := &MockCoordinator{
		stop: make(chan any),
	}
	c.lastCommittedBlockNumber.Store(-1)
	return c
}

// Close closes the mock coordinator.
func (c *MockCoordinator) Close() {
	logger.Infof("Closing mock coordinator")
	close(c.stop)
}

// SetMetaNamespaceVerificationKey sets the verification key.
func (*MockCoordinator) SetMetaNamespaceVerificationKey(
	_ context.Context, _ *protosigverifierservice.Key,
) (*protocoordinatorservice.Empty, error) {
	return &protocoordinatorservice.Empty{}, nil
}

// SetLastCommittedBlockNumber sets the last committed block number.
func (c *MockCoordinator) SetLastCommittedBlockNumber(
	_ context.Context, lastBlock *protoblocktx.BlockInfo,
) (*protocoordinatorservice.Empty, error) {
	c.lastCommittedBlockNumber.Store(int64(lastBlock.Number))
	return &protocoordinatorservice.Empty{}, nil
}

// GetLastCommittedBlockNumber returns the last committed block number.
func (c *MockCoordinator) GetLastCommittedBlockNumber(
	_ context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	return &protoblocktx.BlockInfo{Number: uint64(c.lastCommittedBlockNumber.Load())}, nil
}

// BlockProcessing processes a block.
func (c *MockCoordinator) BlockProcessing(stream protocoordinatorservice.Coordinator_BlockProcessingServer) error {
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

		logger.Debugf("Received block %d with %d transactions", block.Number, len(block.Txs))

		// send to the validation
		input <- block
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

// StartMockCoordinatorService starts a mock coordinator service.
func StartMockCoordinatorService() (*connection.ServerConfig, *MockCoordinator, *grpc.Server) {
	coordinator := NewMockCoordinator()

	sc, grpcSrvs := test.StartMockServers(1, func(server *grpc.Server, _ int) {
		protocoordinatorservice.RegisterCoordinatorServer(server, coordinator)
	})

	return sc[0], coordinator, grpcSrvs[0]
}
