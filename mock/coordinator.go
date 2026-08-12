/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package mock

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/grpcerror"
	"github.com/hyperledger/fabric-x-committer/utils/serve"
)

// Coordinator is a mock implementation of servicepb.CoordinatorServer.
type Coordinator struct {
	servicepb.UnimplementedCoordinatorServer
	nextBlock        atomic.Uint64
	streamActive     atomic.Bool
	numTxsInProgress atomic.Int32
	txsStatus        *fifoCache[*committerpb.TxStatus]
	txsStatusMu      sync.Mutex
	latency          atomic.Pointer[time.Duration]
	healthcheck      *health.Server
}

// We don't want to utilize unlimited memory for storing the transactions status.
// A value of 100,000 TXs is adequate for most of the unit-test.
var defaultTxStatusStorageSize = 100_000

// ErrStreamAlreadyActive is returned when a second block-processing stream is opened.
var ErrStreamAlreadyActive = errors.New("stream is already active. Only one stream is allowed")

// NewMockCoordinator creates a new mock coordinator.
func NewMockCoordinator() *Coordinator {
	return &Coordinator{
		txsStatus:   newFifoCache[*committerpb.TxStatus](defaultTxStatusStorageSize),
		healthcheck: serve.DefaultHealthCheckService(),
	}
}

// RegisterService registers the coordinator's gRPC services.
func (c *Coordinator) RegisterService(s serve.Servers) {
	servicepb.RegisterCoordinatorServer(s.GRPC, c)
	healthgrpc.RegisterHealthServer(s.GRPC, c.healthcheck)
}

// SetLastCommittedBlockNumber sets the last committed block number, so the next
// GetNextBlockNumberToCommit reports the block that follows it.
func (c *Coordinator) SetLastCommittedBlockNumber(
	_ context.Context, lastBlock *servicepb.BlockRef,
) (*emptypb.Empty, error) {
	c.nextBlock.Store(lastBlock.Number + 1)
	return &emptypb.Empty{}, nil
}

// GetNextBlockNumberToCommit returns the next expected block number to be received by the coordinator.
func (c *Coordinator) GetNextBlockNumberToCommit(
	context.Context,
	*emptypb.Empty,
) (*servicepb.BlockRef, error) {
	return &servicepb.BlockRef{Number: c.nextBlock.Load()}, nil
}

// GetTransactionsStatus returns the status of given set of transaction identifiers.
func (c *Coordinator) GetTransactionsStatus(
	_ context.Context,
	q *committerpb.TxIDsBatch,
) (*committerpb.TxStatusBatch, error) {
	status := make([]*committerpb.TxStatus, 0, len(q.TxIds))
	c.txsStatusMu.Lock()
	defer c.txsStatusMu.Unlock()
	for _, txID := range q.TxIds {
		// An unknown TX is omitted rather than reported as a nil element: a nil in a
		// repeated field marshals as a zero-valued TxStatus, which the client cannot
		// distinguish from a genuine status.
		if v, ok := c.txsStatus.get(txID); ok {
			status = append(status, v)
		}
	}
	return &committerpb.TxStatusBatch{Status: status}, nil
}

// NoPendingTransactionProcessing returns true when all previously submitted
// transactions have been processed.
func (c *Coordinator) NoPendingTransactionProcessing(
	context.Context,
	*emptypb.Empty,
) (*wrapperspb.BoolValue, error) {
	return wrapperspb.Bool(c.numTxsInProgress.Load() == 0), nil
}

// IsStreamActive returns true if the stream from the sidecar is active.
func (c *Coordinator) IsStreamActive() bool {
	return c.streamActive.Load()
}

// BlockProcessing processes a block.
func (c *Coordinator) BlockProcessing(stream servicepb.Coordinator_BlockProcessingServer) error {
	if !c.streamActive.CompareAndSwap(false, true) {
		return grpcerror.WrapFailedPrecondition(ErrStreamAlreadyActive)
	}
	defer c.streamActive.CompareAndSwap(true, false)
	logger.Info("Starting block processing stream")
	defer logger.Info("Closed block processing stream")

	g, gCtx := errgroup.WithContext(stream.Context())
	blockQueue := channel.Make[*servicepb.CoordinatorBatch](gCtx, 1000)
	g.Go(func() error {
		return c.receiveBlocks(gCtx, stream, blockQueue)
	})
	g.Go(func() error {
		return c.sendTxsValidationStatus(gCtx, stream, blockQueue)
	})
	return grpcerror.WrapCancelled(g.Wait())
}

func (c *Coordinator) receiveBlocks(
	ctx context.Context,
	stream servicepb.Coordinator_BlockProcessingServer,
	blockQueue channel.Writer[*servicepb.CoordinatorBatch],
) error {
	for ctx.Err() == nil {
		block, err := stream.Recv()
		if err != nil {
			return errors.Wrap(err, "receive block failed")
		}

		if maxBlock, ok := batchBlockNumber(block); ok {
			// Monotonic: a batch never moves the counter backwards. An empty batch carries
			// no TX reference at all (the sidecar maps a block without data to one, see
			// mapBlock), so there is no block number to derive and we must leave the
			// counter alone instead of resetting it to 1.
			c.nextBlock.Store(max(c.nextBlock.Load(), maxBlock+1))
		}

		logger.Debugf("Received batch with %d transactions", len(block.Txs))
		// Rejected TXs are counted too: sendTxsStatusChunk reports a status for each of
		// them, and decrements by the number of statuses it sent. Counting only Txs here
		// would drift the gauge negative and NoPendingTransactionProcessing would never
		// report idle again.
		c.numTxsInProgress.Add(int32(len(block.Txs) + len(block.Rejected))) //nolint:gosec

		// send to the validation
		blockQueue.Write(block)
	}
	return errors.Wrap(ctx.Err(), "context cancelled")
}

// batchBlockNumber returns the highest block number referenced by the batch, and false
// if the batch references no block at all.
func batchBlockNumber(batch *servicepb.CoordinatorBatch) (uint64, bool) {
	var maxBlock uint64
	var found bool
	if len(batch.Txs) > 0 {
		maxBlock = max(maxBlock, batch.Txs[len(batch.Txs)-1].Ref.BlockNum)
		found = true
	}
	if len(batch.Rejected) > 0 {
		maxBlock = max(maxBlock, batch.Rejected[len(batch.Rejected)-1].Ref.BlockNum)
		found = true
	}
	return maxBlock, found
}

func (c *Coordinator) sendTxsValidationStatus(
	ctx context.Context,
	stream servicepb.Coordinator_BlockProcessingServer,
	blockQueue channel.Reader[*servicepb.CoordinatorBatch],
) error {
	for ctx.Err() == nil {
		scBlock, ok := blockQueue.Read()
		if !ok {
			break
		}

		if latency := c.latency.Load(); latency != nil {
			select {
			case <-ctx.Done():
				return errors.Wrap(ctx.Err(), "context cancelled")
			case <-time.After(*latency):
			}
		}

		// Collected into a fresh slice: appending to (and later shuffling) scBlock.Rejected
		// would reorder the received message's own backing array.
		info := make([]*committerpb.TxStatus, 0, len(scBlock.Rejected)+len(scBlock.Txs))
		info = append(info, scBlock.Rejected...)
		for _, tx := range scBlock.Txs {
			info = append(info, &committerpb.TxStatus{
				Ref:    tx.Ref,
				Status: committerpb.Status_COMMITTED,
			})
		}
		rand.Shuffle(len(info), func(i, j int) { info[i], info[j] = info[j], info[i] })

		for len(info) > 0 {
			chunkSize := utils.RandIntN(uint64(len(info))) + 1
			if err := c.sendTxsStatusChunk(stream, info[:chunkSize]); err != nil {
				return err
			}
			info = info[chunkSize:]
		}
	}
	return errors.Wrap(ctx.Err(), "context cancelled")
}

func (c *Coordinator) sendTxsStatusChunk(
	stream servicepb.Coordinator_BlockProcessingServer,
	txs []*committerpb.TxStatus,
) error {
	b := &committerpb.TxStatusBatch{
		Status: make([]*committerpb.TxStatus, len(txs)),
	}
	c.txsStatusMu.Lock()
	defer c.txsStatusMu.Unlock()
	for i, info := range txs {
		s := committerpb.NewTxStatusFromRef(info.Ref, info.Status)
		b.Status[i] = s
		c.txsStatus.addIfNotExist(info.Ref.TxId, s)
	}
	if err := stream.Send(b); err != nil {
		return errors.Wrap(err, "failed to send status")
	}
	logger.Debugf("Sent back batch with %d TXs", len(b.Status))
	c.numTxsInProgress.Add(-int32(len(b.Status))) //nolint:gosec
	return nil
}

// SetTxsInProgress sets the in-progress transaction count. The purpose
// of this method is to set the count manually for testing purposes.
func (c *Coordinator) SetTxsInProgress(count int32) {
	c.numTxsInProgress.Store(count)
}

// SetDelay sets the duration to wait before sending statuses.
func (c *Coordinator) SetDelay(d time.Duration) {
	c.latency.Store(&d)
}
