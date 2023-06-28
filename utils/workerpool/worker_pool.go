package workerpool

import "github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"

var logger = logging.New("worker pool")

type WorkerExecutor = func()

type Config struct {
	//Parallelism How many go routines will be launched and available for the executions
	Parallelism int
	//ChannelCapacity The capacity of the waiting queue for routines that have been submitted,
	//but haven't found an available slot.
	//If all available go routines (Parallelism) are occupied with executions, then the buffer will start to fill.
	//If (Parallelism + ChannelCapacity) executions have been submitted (see WorkerPool.Run),
	//then WorkerPool.Run will block execution until an execution completes and a slot becomes available.
	ChannelCapacity int
}

// TODO: Inline the worker pool

type WorkerPool struct {
	parallelism int
	inputCh     chan WorkerExecutor
}

// New creates a new WorkerPool
func New(config *Config) *WorkerPool {
	if config.Parallelism == 0 {
		panic("parallelism must be > 0")
	}
	w := &WorkerPool{
		inputCh: make(chan WorkerExecutor, config.ChannelCapacity),
	}

	for i := 0; i < config.Parallelism; i++ {
		go w.handleChannelInput(i)
	}
	return w
}

func (w *WorkerPool) handleChannelInput(idx int) {
	for {
		input := <-w.inputCh
		logger.Debugf("Received request %v in go routine %d. Sending for execution.", input, idx)
		input()
	}
}

// Run submits one function for execution in a go routine once a slot is available
func (w *WorkerPool) Run(input func()) {
	w.inputCh <- input
}
