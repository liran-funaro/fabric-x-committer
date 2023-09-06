package vcservicemock

import (
	"errors"
	"io"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
)

// MockVcService implements the protovcservice.ValidationAndCommitServiceServer interface.
// It is used for testing the client which is the coordinator service.
type MockVcService struct {
	protovcservice.UnimplementedValidationAndCommitServiceServer
	txBatchChan        chan *protovcservice.TransactionBatch
	numBatchesReceived *atomic.Uint32
}

// NewMockVcService returns a new MockVcService.
func NewMockVcService() *MockVcService {
	return &MockVcService{
		txBatchChan:        make(chan *protovcservice.TransactionBatch),
		numBatchesReceived: &atomic.Uint32{},
	}
}

// StartValidateAndCommitStream is the mock implementation of the
// protovcservice.ValidationAndCommitServiceServer interface.
func (vc *MockVcService) StartValidateAndCommitStream(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	errorChannel := make(chan error, 2)

	go func() {
		errorChannel <- vc.receiveAndProcessTransactions(stream)
	}()

	go func() {
		errorChannel <- vc.sendTransactionStatus(stream)
	}()

	for i := 0; i < 2; i++ {
		err := <-errorChannel
		if err != nil {
			return err
		}
	}
	return nil
}

func (vc *MockVcService) receiveAndProcessTransactions(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	for {
		txBatch, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		vc.numBatchesReceived.Add(1)
		vc.txBatchChan <- txBatch
	}
}

func (vc *MockVcService) sendTransactionStatus(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	for txBatch := range vc.txBatchChan {
		txsStatus := &protovcservice.TransactionStatus{
			Status: make(map[string]protoblocktx.Status),
		}

		for _, tx := range txBatch.Transactions {
			txsStatus.Status[tx.ID] = protoblocktx.Status_COMMITTED
		}

		err := stream.Send(txsStatus)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}

	return nil
}

// GetNumBatchesReceived returns the number of batches received by MockVcService.
func (vc *MockVcService) GetNumBatchesReceived() uint32 {
	return vc.numBatchesReceived.Load()
}

// Close closes the input channel of MockVcService.
func (vc *MockVcService) Close() {
	close(vc.txBatchChan)
}
