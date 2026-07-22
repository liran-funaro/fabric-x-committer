/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package workload

import (
	"context"
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"golang.org/x/sync/errgroup"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
)

// TxStream yields transactions from the  stream.
type TxStream struct {
	options        *StreamOptions
	gens           []*IndependentTxGenerator
	counter        *atomic.Uint64
	queue          chan []*servicepb.LoadGenTx
	rateController *ConsumerRateController[*servicepb.LoadGenTx]
}

// NewTxStream creates a stream that generates transactions in batches into a queue.
func NewTxStream(profile *Profile, options *StreamOptions) *TxStream {
	queue := make(chan []*servicepb.LoadGenTx, max(options.BuffersSize, 1))
	counter := new(atomic.Uint64)
	return &TxStream{
		options:        options,
		counter:        counter,
		queue:          queue,
		gens:           newIndependentTxGenerators(profile, counter),
		rateController: NewConsumerRateController(options.RateLimit, queue),
	}
}

// KeyStats is a monotonic snapshot of the workload's key-generation counts, derived purely from the
// number of transactions generated so far (N) and the (constant) split configuration. C(N) is the
// committable-write frontier, RO the read-only slot count, and W the write-slot count (read-write +
// blind-write) per transaction. When the split is disabled every slot is a fresh key, so there are no
// references and CreatedKeys counts the fresh write creates (N*W).
type KeyStats struct {
	CreatedKeys         uint64 // committable frontier C(N): keys created by write slots
	ReferencedReadKeys  uint64 // existing (backward) read-only references: N*RO
	ReferencedWriteKeys uint64 // existing (backward) write-slot references: N*W - C(N)
}

// KeyStats returns the current key-generation counts, computed from the shared transaction counter and
// the frontier closed-forms. All fields are monotonic non-decreasing (suitable for counter metrics).
func (s *TxStream) KeyStats() KeyStats {
	if len(s.gens) == 0 {
		return KeyStats{}
	}
	n := s.counter.Load()
	p := s.gens[0].process
	w := uint64(p.ReadWriteCount) + uint64(p.BlindWriteCount)
	if p.NewKeysRate == nil {
		// Split disabled: every slot is a fresh key, so writes create N*W keys and there are no references.
		return KeyStats{CreatedKeys: n * w}
	}
	// Split enabled: writes create up to C(N) keys; all read-only slots and the non-creating write slots
	// are backward references into the working set.
	created := uint64(max(0, p.committedFrontier(n)))
	return KeyStats{
		CreatedKeys:         created,
		ReferencedReadKeys:  n * uint64(p.ReadOnlyCount),
		ReferencedWriteKeys: n*w - created,
	}
}

// Run starts the stream workers.
func (s *TxStream) Run(ctx context.Context) error {
	logger.Debugf("Starting %d workers to generate load", len(s.gens))
	g, gCtx := errgroup.WithContext(ctx)
	for _, gen := range s.gens {
		g.Go(func() error {
			ingestBatchesToQueue(gCtx, s.queue, gen, int(s.options.GenBatch))
			return nil
		})
	}
	return errors.Wrap(g.Wait(), "stream finished")
}

// AppendBatch appends a batch to the stream.
func (s *TxStream) AppendBatch(ctx context.Context, batch []*servicepb.LoadGenTx) {
	channel.NewWriter(ctx, s.queue).Write(batch)
}

// GetRate reads the stream limit.
func (s *TxStream) GetRate() uint64 {
	return s.rateController.Rate()
}

// SetRate sets the stream limit.
func (s *TxStream) SetRate(rate uint64) {
	s.rateController.SetRate(rate)
}

// MakeGenerator creates a new generator that consumes from the stream.
// Each generator must be used from a single goroutine, but different
// generators from the same Stream can be used concurrently.
func (s *TxStream) MakeGenerator() *ConsumerRateController[*servicepb.LoadGenTx] {
	return s.rateController.InstantiateWorker()
}

func ingestBatchesToQueue[T any](ctx context.Context, c chan<- []T, g Generator[T], batchSize int) {
	batchGen := &MultiGenerator[T]{
		Gen:   g,
		Count: max(batchSize, 1),
	}
	q := channel.NewWriter(ctx, c)
	for q.Write(batchGen.Next()) {
	}
}
