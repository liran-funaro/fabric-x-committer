package loadgen

import (
	"context"
	"math/rand"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"go.uber.org/ratelimit"
)

type (
	// TxStream yields transactions from the  stream.
	TxStream = StreamWithGenerator[*protoblocktx.Tx]

	// BlockStream yields blocks from the stream.
	BlockStream = StreamWithGenerator[*protoblocktx.Block]

	// QueryStream generates stream's queries consumers.
	QueryStream = Stream[*protoqueryservice.Query]

	// StreamWithGenerator combines stream information with a generator that yields from the stream.
	StreamWithGenerator[T any] struct {
		Generator[T]
		Stream Stream[T]
		Signer *TxSignerVerifier
		NsID   uint32
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

// StartTxGenerator starts workers that generates transactions into a queue and apply the modifiers.
// Each worker will have a unique instance of the modifier to avoid concurrency issues.
// The modifiers will be applied in the order they are given.
// A transaction modifier can modify any of its fields to adjust the workload.
// For example, a modifier can query the database for the read-set versions to simulate a real transaction.
// The signature modifier is applied last so all previous modifications will be signed correctly.
func StartTxGenerator(
	ctx context.Context, profile *Profile, options *StreamOptions, modifierGenerators ...Generator[Modifier],
) *TxStream {
	logger.Debugf("Starting %d workers to generate load", profile.Workers)

	signer := NewTxSignerVerifier(&profile.Transaction.Signature)
	txQueue := channel.Make[[]*protoblocktx.Tx](ctx, Max(options.BuffersSize, 1))
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
		go ingestBatchesToQueue(txQueue, modGen, int(options.GenBatch))
	}

	stream := Stream[*protoblocktx.Tx]{
		Limiter:    NewLimiter(options.RateLimit),
		BatchQueue: txQueue,
	}
	return &TxStream{
		Generator: stream.MakeGenerator(),
		Stream:    stream,
		Signer:    signer,
	}
}

// StartBlockGenerator starts workers that generates blocks into a queue with modifier.
func StartBlockGenerator(
	ctx context.Context, profile *Profile, options *StreamOptions, modifierGenerators ...Generator[Modifier],
) *BlockStream {
	txGen := StartTxGenerator(ctx, profile, options, modifierGenerators...)
	queue := channel.Make[[]*protoblocktx.Block](ctx, Max(options.BuffersSize, 1))
	// Due to initialization part, the first load block
	// number will be 1.
	txsNum := make([]uint32, profile.Block.Size)
	for i := range profile.Block.Size {
		txsNum[i] = uint32(i) //nolint:gosec
	}
	blockGen := &BlockGenerator{
		TxGenerator: txGen,
		BlockSize:   uint64(profile.Block.Size), //nolint:gosec
		blockNum:    1,
		txNums:      txsNum,
	}
	go ingestBatchesToQueue(queue, blockGen, 1)
	stream := Stream[*protoblocktx.Block]{
		Limiter:    NewLimiter(nil),
		BatchQueue: queue,
	}
	return &BlockStream{
		Generator: stream.MakeGenerator(),
		Stream:    stream,
		Signer:    txGen.Signer,
	}
}

// StartQueryGenerator starts workers that generates queries into a queue.
func StartQueryGenerator(ctx context.Context, profile *Profile, options *StreamOptions) *QueryStream {
	logger.Debugf("Starting %d workers to generate query load", profile.Workers)

	queryQueue := channel.Make[[]*protoqueryservice.Query](ctx, Max(options.BuffersSize, 1))
	for _, w := range makeWorkersData(profile) {
		queryGen := newQueryGenerator(NewRandFromSeedGenerator(w.seed), w.keyGen, profile)
		go ingestBatchesToQueue(queryQueue, queryGen, int(options.GenBatch))
	}

	return &QueryStream{
		Limiter:    NewLimiter(options.RateLimit),
		BatchQueue: queryQueue,
	}
}

type workerData struct {
	seed   *rand.Rand
	keyGen Generator[Key]
}

func makeWorkersData(profile *Profile) []workerData {
	seedGen := rand.New(rand.NewSource(profile.Seed))
	workers := make([]workerData, Max(profile.Workers, 1))
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
		Count: &ConstGenerator[int]{Const: Max(batchSize, 1)},
	}
	for q.Write(batchGen.Next()) {
	}
}
