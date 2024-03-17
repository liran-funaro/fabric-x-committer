package coordinatormock

import (
	"context"
	"io"
	"math/rand"
	"time"

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

// SetVerificationKey sets the verification key.
func (*MockCoordinator) SetVerificationKey(
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
		numParts := 1 + rand.Intn(6)
		perPart := len(scBlock.GetTxs()) / numParts

		for i := 0; i < numParts; i++ {
			scBlock := scBlock
			go func(i int) {
				r := 100 + rand.Intn(1000)
				time.Sleep(time.Duration(r) * time.Microsecond)

				lo := i * perPart
				hi := lo + perPart

				total := len(scBlock.GetTxs())
				b := &protocoordinatorservice.TxValidationStatusBatch{}
				// check if we have a rest
				if total-hi > 0 && total-hi < perPart {
					b.TxsValidationStatus = batch.TxsValidationStatus[lo:]
				} else {
					b.TxsValidationStatus = batch.TxsValidationStatus[lo:hi]
				}

				utils.Must(stream.Send(b))
				logger.Debugf("Sent back batch with %d TXs", len(b.TxsValidationStatus))
			}(i)
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
