package workerpool

import (
	"context"

	"github.com/cockroachdb/errors"
	"golang.org/x/sync/errgroup"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
)

type Config struct {
	// Parallelism How many go routines will be launched and available for the executions
	Parallelism int
	// ChannelCapacity The capacity of the waiting queue for routines that have been submitted,
	// but haven't found an available slot.
	// If all available go routines (Parallelism) are occupied with executions, then the buffer will start to fill.
	// If (Parallelism + ChannelCapacity) executions have been submitted (see WorkerPool.Execute),
	// then WorkerPool.Execute will block execution until an execution completes and a slot becomes available.
	ChannelCapacity int
}

// TODO: Inline the worker pool

// WorkerPool supports parallel task execution with predictable resource consumption.
type WorkerPool struct {
	parallelism int
	inputCh     chan WorkerExecutor
}

// WorkerExecutor function to execute.
type WorkerExecutor = func()

// New creates a new WorkerPool
func New(config *Config) *WorkerPool {
	if config.Parallelism == 0 {
		panic("parallelism must be > 0")
	}
	return &WorkerPool{
		inputCh:     make(chan WorkerExecutor, config.ChannelCapacity),
		parallelism: config.Parallelism,
	}
}

// Run runs the workers.
func (w *WorkerPool) Run(ctx context.Context) error {
	g, gCtx := errgroup.WithContext(ctx)
	for range w.parallelism {
		g.Go(func() error {
			return w.handleChannelInput(gCtx)
		})
	}
	return errors.Wrap(g.Wait(), "worker pool ended")
}

func (w *WorkerPool) handleChannelInput(ctx context.Context) error {
	inputCh := channel.NewReader(ctx, w.inputCh)
	for {
		item, ok := inputCh.Read()
		if !ok {
			return errors.Wrap(ctx.Err(), "context ended")
		}
		item()
	}
}

// Submit submits one function for execution in a go routine once a slot is available.
// Returns true if successful.
func (w *WorkerPool) Submit(ctx context.Context, work WorkerExecutor) bool {
	return channel.NewWriter(ctx, w.inputCh).Write(work)
}

func (w *WorkerPool) Close() {
	close(w.inputCh)
}
