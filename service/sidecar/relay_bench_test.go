/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sidecar

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/loadgen/workload"
	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

// benchBlockSize is the number of TXs per block the relay benchmark feeds. b.N counts
// transactions, not blocks, so ns/op and tx/s stay per transaction.
const benchBlockSize = 500

// BenchmarkRelayThroughput measures the relay's own per-TX cost. It runs the relay's four
// goroutines against a stream stub that echoes a COMMITTED status for every TX it is sent, so
// neither gRPC nor the coordinator is in the measurement — what remains is the relay's TX
// tracking, which touches the TX ID dedup set and the in-flight block window once per TX.
//
// The goroutines are wired here rather than through run, which needs a CoordinatorClient and adds
// setLastCommittedBlockNumber. Keep the wiring below in sync with run's.
//
// Four goroutines and their garbage make this run-to-run noisy — around ±10% — so compare it over
// several -count runs with benchstat rather than reading a single result. BenchmarkMapBlockSize and
// BenchmarkInFlightBlocksWindow isolate the two hot paths with far less spread.
func BenchmarkRelayThroughput(b *testing.B) {
	flogging.ActivateSpec("fatal")
	blocks, statuses := benchBlocksWithStatuses(b, b.N)

	ctx, cancel := context.WithCancel(b.Context())
	defer cancel()

	r := newRelay(time.Hour, newPerformanceMetrics(newQueues(10)))
	incoming := make(chan *common.Block, cap(statuses))
	committed := make(chan *common.Block, len(blocks))
	r.incomingBlockToBeCommitted = incoming
	r.outgoingCommittedBlock = committed
	r.outgoingStatusUpdates = make(chan []*committerpb.TxStatus, len(blocks))
	r.outgoingCommittedBlockWithTxs = make(chan *committedBlockWithTxs, len(blocks))
	r.waitingTxsSlots = utils.NewSlots(int64(4 * benchBlockSize))

	stream := newBenchStream(ctx, statuses)
	mappedBlockQueue := make(chan *blockMappingResult, 8)
	statusBatch := make(chan *committerpb.TxStatusBatch, 8)

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error { return r.preProcessBlock(gCtx, mappedBlockQueue) })
	g.Go(func() error { return r.sendBlocksToCoordinator(gCtx, mappedBlockQueue, stream) })
	g.Go(func() error { return receiveStatusFromCoordinator(gCtx, stream, statusBatch) })
	g.Go(func() error { return r.processStatusBatch(gCtx, statusBatch) })

	incomingWriter := channel.NewWriter(gCtx, incoming)
	committedReader := channel.NewReader(gCtx, committed)

	b.ResetTimer()
	go func() {
		for _, blk := range blocks {
			if !incomingWriter.Write(blk) {
				return
			}
		}
	}()
	for range blocks {
		_, ok := committedReader.Read()
		require.True(b, ok, "the relay stopped before committing every block")
	}
	b.StopTimer()

	cancel()
	require.Error(b, g.Wait(), "the relay must end with the cancelled context error")
	test.ReportTxPerSecond(b)
}

// benchBlocksWithStatuses builds txCount transactions grouped into blocks of at most
// benchBlockSize, together with the status batch the coordinator would return for each block.
func benchBlocksWithStatuses(b *testing.B, txCount int) ([]*common.Block, []*committerpb.TxStatusBatch) {
	b.Helper()
	txs := workload.GenerateTransactions(b, benchTxProfile(), txCount)
	blockCount := (txCount + benchBlockSize - 1) / benchBlockSize
	blocks := make([]*common.Block, 0, blockCount)
	statuses := make([]*committerpb.TxStatusBatch, 0, blockCount)
	for offset := 0; offset < txCount; offset += benchBlockSize {
		blockTxs := txs[offset:min(offset+benchBlockSize, txCount)]
		blockNum := uint64(len(blocks))
		blocks = append(blocks, workload.MapToOrdererBlock(blockNum, blockTxs))

		blockStatus := make([]*committerpb.TxStatus, len(blockTxs))
		for txNum, tx := range blockTxs {
			blockStatus[txNum] = &committerpb.TxStatus{
				Ref:    committerpb.NewTxRef(tx.Id, blockNum, uint32(txNum)),
				Status: committerpb.Status_COMMITTED,
			}
		}
		statuses = append(statuses, &committerpb.TxStatusBatch{Status: blockStatus})
	}
	return blocks, statuses
}

// benchStream stands in for the coordinator's block-processing stream. Send echoes the pre-built
// status batch of the block it is given, so the relay sees a coordinator that commits everything
// instantly. The batches are built before the benchmark starts, so a Send costs nothing beyond
// the handoff. Send is only ever called by sendBlocksToCoordinator and Recv only by
// receiveStatusFromCoordinator, so sent needs no synchronization.
type benchStream struct {
	grpc.ClientStream
	ctx      context.Context //nolint:containedctx // a stream stub has to return a context.
	statuses []*committerpb.TxStatusBatch
	sent     int
	echoW    channel.Writer[*committerpb.TxStatusBatch]
	echoR    channel.Reader[*committerpb.TxStatusBatch]
}

func newBenchStream(ctx context.Context, statuses []*committerpb.TxStatusBatch) *benchStream {
	echo := make(chan *committerpb.TxStatusBatch, 8)
	return &benchStream{
		ctx:      ctx,
		statuses: statuses,
		echoW:    channel.NewWriter(ctx, echo),
		echoR:    channel.NewReader(ctx, echo),
	}
}

func (s *benchStream) Send(*servicepb.CoordinatorBatch) error {
	if s.sent == len(s.statuses) {
		return errors.New("the relay sent more batches than there are blocks")
	}
	s.echoW.Write(s.statuses[s.sent])
	s.sent++
	return nil
}

func (s *benchStream) Recv() (*committerpb.TxStatusBatch, error) {
	status, ok := s.echoR.Read()
	if !ok {
		return nil, errors.Wrap(s.ctx.Err(), "context ended")
	}
	return status, nil
}

func (s *benchStream) Context() context.Context {
	return s.ctx
}

// benchWindowDepth is how many blocks the window benchmark keeps in flight. The relay's window is
// bounded by the block channels, each sized by ChannelBufferSize (default 100).
const benchWindowDepth = 200

// BenchmarkInFlightBlocksWindow measures one block's round trip through the window — register, a
// lookup per TX, then retire — at the depth the relay's channels allow.
func BenchmarkInFlightBlocksWindow(b *testing.B) {
	var blocks inFlightBlocks
	blocks.reset(0)
	tracked := &blockWithStatus{}

	// Fill the window so every measured iteration runs at full depth.
	for i := range uint64(benchWindowDepth) {
		_, err := blocks.register(i, tracked)
		require.NoError(b, err)
	}

	b.ResetTimer()
	for i := range uint64(b.N) { //nolint:gosec // b.N is never negative.
		next := i + benchWindowDepth
		_, err := blocks.register(next, tracked)
		require.NoError(b, err)
		for range benchBlockSize {
			if blocks.get(next) == nil {
				b.Fatal("the registered block must be tracked")
			}
		}
		if blocks.first() == nil {
			b.Fatal("the window must not be empty")
		}
		blocks.dropFirst()
	}
	b.StopTimer()
}
