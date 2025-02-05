package workload

import (
	"context"
	"math/rand"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"go.uber.org/ratelimit"
	"golang.org/x/sync/errgroup"
)

type (
	// TxStream yields transactions from the  stream.
	TxStream struct {
		Stream[*protoblocktx.Tx]
		gens    []*txModifierDecorator
		txQueue chan []*protoblocktx.Tx
		options *StreamOptions
		ready   chan any
	}

	// QueryStream generates stream's queries consumers.
	QueryStream struct {
		Stream[*protoqueryservice.Query]
		gen     []*QueryGenerator
		queue   chan []*protoqueryservice.Query
		options *StreamOptions
		ready   chan any
	}

	// Stream makes generators that consume from a stream (BatchQueue).
	// Each generator cannot be used concurrently by different goroutines.
	// To consume the stream by multiple goroutines, generate a consumer
	// for each goroutine.
	Stream[T any] struct {
		Limiter    ratelimit.Limiter
		BatchQueue channel.ReaderWriter[[]T]
	}
)

// NewTxStream creates workers that generates transactions into a queue and apply the modifiers.
// Each worker will have a unique instance of the modifier to avoid concurrency issues.
// The modifiers will be applied in the order they are given.
// A transaction modifier can modify any of its fields to adjust the workload.
// For example, a modifier can query the database for the read-set versions to simulate a real transaction.
// The signature modifier is applied last so all previous modifications will be signed correctly.
func NewTxStream(
	profile *Profile,
	options *StreamOptions,
	modifierGenerators ...Generator[Modifier],
) *TxStream {
	signer := NewTxSignerVerifier(profile.Transaction.Policy)
	txStream := &TxStream{
		ready:   make(chan any),
		txQueue: make(chan []*protoblocktx.Tx, max(options.BuffersSize, 1)),
		options: options,
	}
	for _, w := range makeWorkersData(profile) {
		modifiers := make([]Modifier, 0, len(modifierGenerators)+2)
		if len(profile.Conflicts.Dependencies) > 0 {
			modifiers = append(modifiers, newTxDependenciesModifier(NewRandFromSeedGenerator(w.seed), profile))
		}
		for _, mod := range modifierGenerators {
			modifiers = append(modifiers, mod.Next())
		}
		// The signer must be the last modifier.
		modifiers = append(modifiers, newSignTxModifier(NewRandFromSeedGenerator(w.seed), signer, profile))

		txGen := newIndependentTxGenerator(NewRandFromSeedGenerator(w.seed), w.keyGen, &profile.Transaction)
		modGen := newTxModifierTxDecorator(txGen, modifiers...)
		txStream.gens = append(txStream.gens, modGen)
	}
	return txStream
}

// WaitForReady waits for the service resources to initialize, so it is ready to answers requests.
// If the context ended before the service is ready, returns false.
func (s *TxStream) WaitForReady(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-s.ready:
		return true
	}
}

// Run starts the stream workers.
func (s *TxStream) Run(ctx context.Context) error {
	logger.Debugf("Starting %d workers to generate load", len(s.gens))

	g, gCtx := errgroup.WithContext(ctx)
	txQueue := channel.NewReaderWriter(gCtx, s.txQueue)
	for _, gen := range s.gens {
		g.Go(func() error {
			ingestBatchesToQueue(txQueue, gen, int(s.options.GenBatch))
			return nil
		})
	}

	s.Stream = Stream[*protoblocktx.Tx]{
		Limiter:    NewLimiter(s.options.RateLimit),
		BatchQueue: txQueue,
	}
	close(s.ready)
	return g.Wait()
}

// MakeGenerator creates a new generator that consumes from the stream.
// Each generator must be used from a single goroutine, but different
// generators from the same Stream can be used concurrently.
func (s *Stream[T]) MakeGenerator() Generator[T] {
	return &RateLimiterGenerator[T]{
		Generator: &BatchChanGenerator[T]{
			Chan: s.BatchQueue,
		},
		Limiter: s.Limiter,
	}
}

// NewQueryGenerator creates workers that generates queries into a queue.
func NewQueryGenerator(profile *Profile, options *StreamOptions) *QueryStream {
	qs := &QueryStream{
		options: options,
		queue:   make(chan []*protoqueryservice.Query, max(options.BuffersSize, 1)),
		ready:   make(chan any),
	}
	for _, w := range makeWorkersData(profile) {
		queryGen := newQueryGenerator(NewRandFromSeedGenerator(w.seed), w.keyGen, profile)
		qs.gen = append(qs.gen, queryGen)
	}
	return qs
}

// WaitForReady waits for the service resources to initialize, so it is ready to answers requests.
// If the context ended before the service is ready, returns false.
func (s *QueryStream) WaitForReady(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-s.ready:
		return true
	}
}

// Run starts the workers.
func (s *QueryStream) Run(ctx context.Context) error {
	logger.Debugf("Starting %d workers to generate query load", len(s.gen))

	g, gCtx := errgroup.WithContext(ctx)
	queue := channel.NewReaderWriter(gCtx, s.queue)
	for _, gen := range s.gen {
		g.Go(func() error {
			ingestBatchesToQueue(queue, gen, int(s.options.GenBatch))
			return nil
		})
	}
	s.Stream = Stream[*protoqueryservice.Query]{
		Limiter:    NewLimiter(s.options.RateLimit),
		BatchQueue: queue,
	}
	close(s.ready)
	return g.Wait()
}

type workerData struct {
	seed   *rand.Rand
	keyGen Generator[Key]
}

func makeWorkersData(profile *Profile) []workerData {
	seedGen := rand.New(rand.NewSource(profile.Seed))
	workers := make([]workerData, max(profile.Workers, 1))
	for i := range workers {
		seed := NewRandFromSeedGenerator(seedGen)
		// Each worker has a unique seed to generate keys in addition the seed for the other content.
		// This allows reproducing the generated keys regardless of the other generated content.
		// It is useful when generating transactions, and later generating queries for the same keys.
		keySeed := NewRandFromSeedGenerator(seed)
		workers[i] = workerData{
			seed:   seed,
			keyGen: &ByteArrayGenerator{Size: profile.Key.Size, Rnd: keySeed},
		}
	}
	return workers
}

func ingestBatchesToQueue[T any](q channel.Writer[[]T], g Generator[T], batchSize int) {
	batchGen := &MultiGenerator[T]{
		Gen:   g,
		Count: &ConstGenerator[int]{Const: max(batchSize, 1)},
	}
	for q.Write(batchGen.Next()) {
	}
}
