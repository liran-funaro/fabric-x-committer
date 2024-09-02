package coordinatormock

import (
	"context"
	"io"
	"math/rand"

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

// NewMockCoordinator creates a new mock coordinator.
func NewMockCoordinator() *MockCoordinator {
	return &MockCoordinator{stop: make(chan any)}
}

// MockCoordinator is a mock coordinator.
type MockCoordinator struct {
	protocoordinatorservice.UnimplementedCoordinatorServer
	stop chan any
}

// Close closes the mock coordinator.
func (s *MockCoordinator) Close() {
	logger.Infof("Closing mock coordinator")
	close(s.stop)
}

// SetMetaNamespaceVerificationKey sets the verification key.
func (*MockCoordinator) SetMetaNamespaceVerificationKey(
	_ context.Context, _ *protosigverifierservice.Key,
) (*protocoordinatorservice.Empty, error) {
	return &protocoordinatorservice.Empty{}, nil
}

// BlockProcessing processes a block.
func (s *MockCoordinator) BlockProcessing(stream protocoordinatorservice.Coordinator_BlockProcessingServer) error {
	input := make(chan *protoblocktx.Block, 1000)
	defer close(input)
	defer logger.Infof("Closed mock coordinator")

	go s.sendTxsValidationStatus(stream, input)

	// start listening
	for {
		select {
		case <-s.stop:
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

func (*MockCoordinator) sendTxsValidationStatus(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer, input chan *protoblocktx.Block,
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
