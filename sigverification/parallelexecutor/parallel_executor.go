package parallelexecutor

import (
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type Input = sigverification.Request
type Output = sigverification.Response
type ExecutorFunc = func(*Input) (*Output, error)

var logger = logging.New("parallel_executor")

type Config struct {
	//Parallelism How many parallel go routines will be launched
	Parallelism int `mapstructure:"parallelism"`
	//BatchSizeCutoff The minimum amount of responses we need to collect before emitting a response
	BatchSizeCutoff int `mapstructure:"batch-size-cutoff"`
	//BatchTimeCutoff How often we should empty the non-empty buffer
	BatchTimeCutoff time.Duration `mapstructure:"batch-time-cutoff"`
	//ChannelBufferSize The size of the buffer of the input channels (increase for high fluctuations of load)
	ChannelBufferSize int `mapstructure:"channel-buffer-size"`
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
	inputCh             chan *Input
	outputCh            chan []*Output
	errorCh             chan error
	batchManualCutoffCh chan struct{}
	outputAggregationCh chan *Output
	executor            ExecutorFunc
	batchSizeCutoff     int
	batchTimeCutoff     time.Duration
	metrics             *metrics.Metrics
}

func New(executor ExecutorFunc, config *Config, metrics *metrics.Metrics) ParallelExecutor {
	channelCapacity := config.ChannelBufferSize * config.Parallelism
	if metrics.Enabled {
		metrics.ParallelExecutorInputChLength.SetCapacity(channelCapacity)
		metrics.ParallelExecutorOutputChLength.SetCapacity(channelCapacity)
	}
	e := &parallelExecutor{
		inputCh:             make(chan *Input, channelCapacity),
		outputCh:            make(chan []*Output),
		errorCh:             make(chan error, config.Parallelism),
		batchManualCutoffCh: make(chan struct{}),
		outputAggregationCh: make(chan *Output, channelCapacity),
		executor:            executor,
		batchSizeCutoff:     config.BatchSizeCutoff,
		batchTimeCutoff:     config.BatchTimeCutoff,
		metrics:             metrics,
	}

	go e.handleTimeManualCutoff()
	for i := 0; i < config.Parallelism; i++ {
		go e.handleChannelInput(e.inputCh, i)
	}

	logger.Infof("Was created and initialized with:\n\tParallelism:\t\t%d\n\tBatchSizeCutoff:\t%d\n\tBatchTimeCutoff:\t%v\n\tChannelBufferSize:\t%d",
		config.Parallelism, config.BatchSizeCutoff, config.BatchTimeCutoff, config.ChannelBufferSize)
	return e
}

func (e *parallelExecutor) handleChannelInput(channel chan *Input, idx int) {
	for {
		input := <-channel
		logger.Debugf("Received request %v with %d inputs and %d outputs in go routine %d. Sending for execution.", input, len(input.Tx.SerialNumbers), len(input.Tx.Outputs), idx)
		output, err := e.executor(input)
		if err != nil {
			logger.Debugf("Received error from executor %d.", idx)
			e.errorCh <- err
		} else {
			logger.Debugf("Received output from executor %d: %v", idx, output)
			e.outputAggregationCh <- output
		}
		if e.metrics.Enabled {
			e.metrics.ParallelExecutorInputChLength.Set(len(channel))
			e.metrics.ParallelExecutorOutputChLength.Set(len(e.outputAggregationCh))
		}
	}
}

func (e *parallelExecutor) handleTimeManualCutoff() {
	var outputBuffer []*Output
	for {
		select {
		case <-e.batchManualCutoffCh:
			logger.Debugf("Attempts to cut a batch, because it was requested manually. (Buffer size: %d)", len(outputBuffer))
			outputBuffer = e.cutBatch(outputBuffer, 1)
		case <-time.After(e.batchTimeCutoff):
			//logger.Debugf("Attempts to cut a batch, because the timer expired. (Buffer size: %d)", len(outputBuffer))
			outputBuffer = e.cutBatch(outputBuffer, 1)
		case output := <-e.outputAggregationCh:
			logger.Debugf("Attempts to emit a batch, because a go routine finished a calculation. (Buffer size: %d)", len(outputBuffer)+1)
			outputBuffer = e.cutBatch(append(outputBuffer, output), e.batchSizeCutoff)
		}
	}
}

func (e *parallelExecutor) cutBatch(buffer []*Output, minBatchSize int) []*Output {
	if len(buffer) < minBatchSize {
		if e.metrics.Enabled {
			for _, tx := range buffer {
				e.metrics.RequestTracer.AddEvent(token.TxSeqNum{tx.BlockNum, tx.TxNum}, "Too small batch. Postponing batch cut.")
			}
		}
		return buffer
	}
	batchSize := utils.Min(e.batchSizeCutoff, len(buffer))
	logger.Debugf("Cuts batch with %d/%d of the outputs.", batchSize, len(buffer))
	if e.metrics.Enabled {
		e.metrics.ParallelExecutorOutTxs.Add(batchSize)
		for _, tx := range buffer {
			e.metrics.RequestTracer.AddEvent(token.TxSeqNum{tx.BlockNum, tx.TxNum}, "Cutting batch. Will send TX to output.")
		}
	}
	e.outputCh <- buffer[:batchSize]
	return e.cutBatch(buffer[batchSize:], minBatchSize)
}

func (e *parallelExecutor) Outputs() <-chan []*Output {
	return e.outputCh
}

func (e *parallelExecutor) Errors() <-chan error {
	return e.errorCh
}

func (e *parallelExecutor) Submit(inputs []*Input) {
	logger.Debugf("Will submit %d requests for execution.", len(inputs))
	for _, input := range inputs {
		e.inputCh <- input
	}
	if e.metrics.Enabled {
		e.metrics.ParallelExecutorInTxs.Add(len(inputs))
	}
}

//CutBatch cuts a new batch regardless of the size (if not empty)
func (e *parallelExecutor) CutBatch() {
	e.batchManualCutoffCh <- struct{}{}
}
