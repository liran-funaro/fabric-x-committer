package mock

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"golang.org/x/sync/errgroup"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/grpcerror"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

// VcService implements the protovcservice.ValidationAndCommitServiceServer interface.
// It is used for testing the client which is the coordinator service.
type VcService struct {
	protovcservice.ValidationAndCommitServiceServer
	txBatchChan        chan *protovcservice.TransactionBatch
	numBatchesReceived *atomic.Uint32
	lastCommittedBlock atomic.Int64
	txsStatus          *sync.Map
	// MockFaultyNodeDropSize allows mocking a faulty node by dropping some TXs.
	MockFaultyNodeDropSize int
}

// NewMockVcService returns a new VcService.
func NewMockVcService() *VcService {
	m := &VcService{
		txBatchChan:        make(chan *protovcservice.TransactionBatch),
		numBatchesReceived: &atomic.Uint32{},
		txsStatus:          &sync.Map{},
	}
	m.lastCommittedBlock.Store(-1)

	return m
}

// SetLastCommittedBlockNumber set the last committed block number in the database/ledger.
func (vc *VcService) SetLastCommittedBlockNumber(
	_ context.Context,
	lastBlock *protoblocktx.BlockInfo,
) (*protovcservice.Empty, error) {
	vc.lastCommittedBlock.Store(int64(lastBlock.Number)) // nolint:gosec
	return nil, nil
}

// GetLastCommittedBlockNumber get the last committed block number in the database/ledger.
func (vc *VcService) GetLastCommittedBlockNumber(
	_ context.Context,
	_ *protovcservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	if vc.lastCommittedBlock.Load() == -1 {
		return nil, grpcerror.WrapNotFound(vcservice.ErrMetadataEmpty)
	}
	return &protoblocktx.BlockInfo{Number: uint64(vc.lastCommittedBlock.Load())}, nil // nolint:gosec
}

// GetPolicies is a mock implementation of the protovcservice.GetPolicies.
func (*VcService) GetPolicies(
	context.Context,
	*protovcservice.Empty,
) (*protoblocktx.Policies, error) {
	return &protoblocktx.Policies{}, nil
}

// GetTransactionsStatus get the status for a given set of transactions IDs.
func (vc *VcService) GetTransactionsStatus(
	_ context.Context,
	query *protoblocktx.QueryStatus,
) (*protoblocktx.TransactionsStatus, error) {
	s := &protoblocktx.TransactionsStatus{Status: make(map[string]*protoblocktx.StatusWithHeight)}
	for _, id := range query.TxIDs {
		v, ok := vc.txsStatus.Load(id)
		if ok {
			s.Status[id] = &protoblocktx.StatusWithHeight{Code: v.(protoblocktx.Status)} //nolint
		}
	}

	return s, nil
}

// StartValidateAndCommitStream is the mock implementation of the
// protovcservice.ValidationAndCommitServiceServer interface.
func (vc *VcService) StartValidateAndCommitStream(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	g, gCtx := errgroup.WithContext(stream.Context())
	g.Go(func() error {
		return vc.receiveAndProcessTransactions(gCtx, stream)
	})
	g.Go(func() error {
		return vc.sendTransactionStatus(gCtx, stream)
	})
	return errors.Wrap(g.Wait(), "VC ended")
}

func (vc *VcService) receiveAndProcessTransactions(
	ctx context.Context, stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	txBatchChan := channel.NewWriter(ctx, vc.txBatchChan)
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

		vc.numBatchesReceived.Add(1)

		if !txBatchChan.Write(txBatch) {
			return nil
		}
	}
}

func (vc *VcService) sendTransactionStatus(
	ctx context.Context, stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	txBatchChan := channel.NewReader(ctx, vc.txBatchChan)
	for {
		txBatch, ok := txBatchChan.Read()
		if !ok {
			return nil
		}
		txsStatus := &protoblocktx.TransactionsStatus{
			Status: make(map[string]*protoblocktx.StatusWithHeight),
		}

		// We simulate a faulty node by not responding to the first X TXs.
		for i, tx := range txBatch.Transactions {
			if i < vc.MockFaultyNodeDropSize {
				continue
			}
			code := protoblocktx.Status_COMMITTED
			if tx.PrelimInvalidTxStatus != nil {
				code = tx.PrelimInvalidTxStatus.Code
			}
			txsStatus.Status[tx.ID] = types.CreateStatusWithHeight(code, tx.BlockNumber, int(tx.TxNum))
			vc.txsStatus.Store(tx.ID, code)
		}

		err := stream.Send(txsStatus)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// GetNumBatchesReceived returns the number of batches received by VcService.
func (vc *VcService) GetNumBatchesReceived() uint32 {
	return vc.numBatchesReceived.Load()
}
