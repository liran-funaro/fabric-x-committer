package vcservicemock

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"google.golang.org/grpc"
)

// MockVcService implements the protovcservice.ValidationAndCommitServiceServer interface.
// It is used for testing the client which is the coordinator service.
type MockVcService struct {
	protovcservice.ValidationAndCommitServiceServer
	txBatchChan            chan *protovcservice.TransactionBatch
	numBatchesReceived     *atomic.Uint32
	lastCommittedBlock     atomic.Int64
	numWaitingTransactions atomic.Int32
	maxBlockNumber         atomic.Int64
	txsStatus              *sync.Map
}

// NewMockVcService returns a new MockVcService.
func NewMockVcService() *MockVcService {
	m := &MockVcService{
		txBatchChan:        make(chan *protovcservice.TransactionBatch),
		numBatchesReceived: &atomic.Uint32{},
		txsStatus:          &sync.Map{},
	}
	m.lastCommittedBlock.Store(-1)
	m.maxBlockNumber.Store(-1)

	return m
}

// NumberOfWaitingTransactionsForStatus returns the number of transactions waiting to get the final status.
func (vc *MockVcService) NumberOfWaitingTransactionsForStatus(
	_ context.Context,
	_ *protovcservice.Empty,
) (*protovcservice.WaitingTransactions, error) {
	return &protovcservice.WaitingTransactions{Count: vc.numWaitingTransactions.Load()}, nil
}

// SetLastCommittedBlockNumber set the last committed block number in the database/ledger.
func (vc *MockVcService) SetLastCommittedBlockNumber(
	_ context.Context,
	lastBlock *protoblocktx.BlockInfo,
) (*protovcservice.Empty, error) {
	vc.lastCommittedBlock.Store(int64(lastBlock.Number)) // nolint:gosec
	return nil, nil
}

// GetLastCommittedBlockNumber get the last committed block number in the database/ledger.
func (vc *MockVcService) GetLastCommittedBlockNumber(
	_ context.Context,
	_ *protovcservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	if vc.lastCommittedBlock.Load() == -1 {
		return nil, vcservice.ErrMetadataEmpty
	}
	return &protoblocktx.BlockInfo{Number: uint64(vc.lastCommittedBlock.Load())}, nil // nolint:gosec
}

// GetMaxSeenBlockNumber get the last seen maximum block number in the database/ledger.
func (vc *MockVcService) GetMaxSeenBlockNumber(
	_ context.Context,
	_ *protovcservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	if vc.maxBlockNumber.Load() == -1 {
		return nil, vcservice.ErrMetadataEmpty
	}
	return &protoblocktx.BlockInfo{Number: uint64(vc.maxBlockNumber.Load())}, nil // nolint:gosec
}

// GetTransactionsStatus get the status for a given set of transactions IDs.
func (vc *MockVcService) GetTransactionsStatus(
	_ context.Context,
	query *protoblocktx.QueryStatus,
) (*protovcservice.TransactionStatus, error) {
	s := &protovcservice.TransactionStatus{Status: make(map[string]protoblocktx.Status)}
	for _, id := range query.TxIDs {
		v, ok := vc.txsStatus.Load(id)
		if ok {
			s.Status[id] = v.(protoblocktx.Status) //nolint
		}
	}

	return s, nil
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

		preTxNum := txBatch.Transactions[0].TxNum
		for _, tx := range txBatch.Transactions[1:] {
			if preTxNum == tx.TxNum {
				return errors.New("duplication tx num detected")
			}
		}

		vc.numWaitingTransactions.Add(int32(len(txBatch.Transactions))) // nolint:gosec
		vc.numBatchesReceived.Add(1)
		vc.txBatchChan <- txBatch
	}
}

func (vc *MockVcService) sendTransactionStatus(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	txBatchChan := channel.NewReader(stream.Context(), vc.txBatchChan)
	for {
		txBatch, ok := txBatchChan.Read()
		if !ok {
			return nil
		}
		txsStatus := &protovcservice.TransactionStatus{
			Status: make(map[string]protoblocktx.Status),
		}

		maxNum := vc.lastCommittedBlock.Load()
		for _, tx := range txBatch.Transactions {
			if tx.PrelimInvalidTxStatus != nil {
				txsStatus.Status[tx.ID] = tx.PrelimInvalidTxStatus.Code
				vc.txsStatus.Store(tx.ID, tx.PrelimInvalidTxStatus.Code)
			} else {
				txsStatus.Status[tx.ID] = protoblocktx.Status_COMMITTED
				vc.txsStatus.Store(tx.ID, protoblocktx.Status_COMMITTED)
			}
			maxNum = max(maxNum, int64(tx.BlockNumber)) // nolint:gosec
		}
		vc.maxBlockNumber.Store(maxNum)

		err := stream.Send(txsStatus)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		vc.numWaitingTransactions.Add(-int32(len(txBatch.Transactions))) // nolint:gosec
	}
}

// GetNumBatchesReceived returns the number of batches received by MockVcService.
func (vc *MockVcService) GetNumBatchesReceived() uint32 {
	return vc.numBatchesReceived.Load()
}

// Close closes the input channel of MockVcService.
func (vc *MockVcService) Close() {
	close(vc.txBatchChan)
}

// StartMockVCService starts a specified number of mock VC service.
func StartMockVCService(numService int) ([]*connection.ServerConfig, []*MockVcService, []*grpc.Server) {
	vcs := make([]*MockVcService, numService)
	for i := 0; i < numService; i++ {
		vcs[i] = NewMockVcService()
	}

	sc, grpcSrvs := test.StartMockServers(numService, func(server *grpc.Server, index int) {
		protovcservice.RegisterValidationAndCommitServiceServer(server, vcs[index])
	})

	return sc, vcs, grpcSrvs
}
