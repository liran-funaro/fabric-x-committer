package sigverification

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"log"
	"time"
)

type Input = Request
type Output = Response

type executorFunc = func(*Input) (*Output, error)

type ParallelExecutionConfig struct {
	//Parallelism How many parallel go routines will be launched
	Parallelism int
	//BatchSizeCutoff The minimum amount of responses we need to collect before emitting a response
	BatchSizeCutoff int
	//BatchTimeCutoff How often we should empty the non-empty buffer
	BatchTimeCutoff time.Duration
	//ChannelBufferSize The size of the buffer of the input channels (increase for high fluctuations of load)
	ChannelBufferSize int
}

type ParallelExecutor interface {
	//Submit multiple requests that will be distributed to various go routines for execution
	//The responses will be aggregated and buffered.
	//When a sufficient amount of responses is collected, it will be emitted from the Outputs channel.
	Submit([]*Input)
	//Outputs returns the emitter channel that returns batches of responses of size BatchSizeCutoff
	//The batches can be even smaller when we cut the batch manually or the batch timeout expires.
	Outputs() <-chan []*Output
	//Errors returns any error that happened during the execution
	Errors() <-chan error
}

//TODO: A channel-closing mechanism may be necessary for the future
type parallelExecutor struct {
	currentInputChIdx   int
	inputChs            []chan *Input
	outputCh            chan []*Output
	errorCh             chan error
	batchManualCutoffCh chan struct{}
	outputAggregationCh chan *Output
	outputBuffer        []*Output
	executor            executorFunc
	batchSizeCutoff     int
	batchTimeCutoff     time.Duration
}

func NewParallelExecutor(executor executorFunc, config *ParallelExecutionConfig) ParallelExecutor {
	inputChs := make([]chan *Input, config.Parallelism)
	for i := 0; i < config.Parallelism; i++ {
		inputChs[i] = make(chan *Input, config.ChannelBufferSize)
	}
	e := &parallelExecutor{
		currentInputChIdx:   0,
		inputChs:            inputChs,
		outputCh:            make(chan []*Output),
		errorCh:             make(chan error, config.Parallelism),
		batchManualCutoffCh: make(chan struct{}),
		outputAggregationCh: make(chan *Output, config.ChannelBufferSize*config.Parallelism),
		outputBuffer:        []*Output{},
		executor:            executor,
		batchSizeCutoff:     config.BatchSizeCutoff,
		batchTimeCutoff:     config.BatchTimeCutoff,
	}

	go e.handleTimeManualCutoff()
	for i, ch := range e.inputChs {
		go e.handleChannelInput(ch, i)
	}

	return e
}

func (e *parallelExecutor) handleChannelInput(channel chan *Input, idx int) {
	for {
		input := <-channel
		log.Printf("Received input %v in channel %d", input, idx)
		output, err := e.executor(input)
		if err != nil {
			e.errorCh <- err
		} else {
			e.outputAggregationCh <- output
		}
	}
}

func (e *parallelExecutor) handleTimeManualCutoff() {
	var outputBuffer []*Output
	for {
		select {
		case <-e.batchManualCutoffCh:
			outputBuffer = e.cutBatch(outputBuffer, 1)
		case <-time.After(e.batchTimeCutoff):
			outputBuffer = e.cutBatch(outputBuffer, 1)
		case output := <-e.outputAggregationCh:
			outputBuffer = e.cutBatch(append(outputBuffer, output), e.batchSizeCutoff)
		}
	}
}

func (e *parallelExecutor) cutBatch(buffer []*Output, minBatchSize int) []*Output {
	if len(buffer) < minBatchSize {
		return buffer
	}
	batchSize := utils.Min(e.batchSizeCutoff, len(buffer))
	e.outputCh <- buffer[:batchSize]
	return buffer[batchSize:]
}

func (e *parallelExecutor) Outputs() <-chan []*Output {
	return e.outputCh
}

func (e *parallelExecutor) Errors() <-chan error {
	return e.errorCh
}

func (e *parallelExecutor) Submit(inputs []*Input) {
	for _, input := range inputs {
		e.nextInputCh() <- input
	}
}

//CutBatch cuts a new batch regardless of the size (if not empty)
func (e *parallelExecutor) CutBatch() {
	e.batchManualCutoffCh <- struct{}{}
}

func (e *parallelExecutor) nextInputCh() chan *Input {
	next := e.inputChs[e.currentInputChIdx]
	e.currentInputChIdx = (e.currentInputChIdx + 1) % len(e.inputChs)
	return next
}
