package testutils

import (
	"context"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/streamhandler"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

// Tx

type TxGenerator struct {
	signTx    func([]signature.SerialNumber) *token.Tx
	PublicKey signature.PublicKey

	txSizeGenerator           *test.PositiveIntGenerator
	serialNumberSizeGenerator *test.PositiveIntGenerator
	serialNumberGenerator     *test.FastByteArrayGenerator
	validSigRatioGenerator    *test.BooleanGenerator
}

type TxGeneratorParams struct {
	Scheme           signature.Scheme
	ValidSigRatio    test.Percentage
	TxSize           test.Distribution
	SerialNumberSize test.Distribution
}

func NewTxGenerator(params *TxGeneratorParams) *TxGenerator {
	signTx, verificationKey := signature.NewSignerVerifier(params.Scheme)
	return &TxGenerator{
		signTx:                    signTx,
		PublicKey:                 verificationKey,
		txSizeGenerator:           test.NewPositiveIntGenerator(params.TxSize, 50),
		serialNumberGenerator:     test.NewFastByteArrayGenerator(test.ConstantByteGenerator, params.SerialNumberSize, 60),
		serialNumberSizeGenerator: test.NewPositiveIntGenerator(params.SerialNumberSize, 30),
		validSigRatioGenerator:    test.NewBooleanGenerator(test.PercentageUniformDistribution, params.ValidSigRatio, 10),
	}
}

func (g *TxGenerator) Next() *token.Tx {
	serialNumbers := g.nextSerialNumbers()
	if g.validSigRatioGenerator.Next() {
		return g.signTx(serialNumbers)
	}
	return &token.Tx{SerialNumbers: serialNumbers}
}

func (g *TxGenerator) nextSerialNumbers() []signature.SerialNumber {
	txSize := g.txSizeGenerator.Next()
	serialNumbers := make([]signature.SerialNumber, txSize)
	for i := 0; i < txSize; i++ {
		serialNumbers[i] = g.serialNumberGenerator.NextWithSize(g.serialNumberSizeGenerator.Next())
	}
	return serialNumbers
}

// TX request batch

type RequestBatchGeneratorParams struct {
	Tx        *TxGeneratorParams
	BatchSize test.Distribution
}
type RequestBatchGenerator struct {
	signature.PublicKey
	inputArrayGenerator *FastInputArrayGenerator
	batchSizeGenerator  *test.PositiveIntGenerator
}

func NewRequestBatchGenerator(params *RequestBatchGeneratorParams, sampleSize int) *RequestBatchGenerator {
	txGenerator := NewTxGenerator(params.Tx)
	valueGen := func() *sigverification.Request {
		return &sigverification.Request{
			BlockNum: 0,
			TxNum:    0,
			Tx:       txGenerator.Next(),
		}
	}
	return &RequestBatchGenerator{
		PublicKey:           txGenerator.PublicKey,
		inputArrayGenerator: NewFastInputSliceGenerator(valueGen, sampleSize),
		batchSizeGenerator:  test.NewPositiveIntGenerator(params.BatchSize, 30),
	}
}

func (c *RequestBatchGenerator) Next() []*sigverification.Request {
	return c.inputArrayGenerator.NextWithSize(c.batchSizeGenerator.Next())
}

type T = *sigverification.Response

// Code identical to test.FastByteArrayGenerator
type FastOutputArrayGenerator struct {
	sample             []T
	batchSizeGenerator *test.PositiveIntGenerator
}

func NewFastOutputSliceGenerator(valueGenerator func() T, sampleSize int) *FastOutputArrayGenerator {
	sample := make([]T, sampleSize)
	for i := 0; i < sampleSize; i++ {
		sample[i] = valueGenerator()
	}
	return &FastOutputArrayGenerator{sample: sample}
}

func (g *FastOutputArrayGenerator) NextWithSize(targetSize int) []T {
	var batch []T
	for remaining := targetSize; remaining > 0; remaining = targetSize - len(batch) {
		batch = append(batch, g.sample[:utils.Min(len(g.sample), remaining)]...)
	}
	return batch
}

type S = *sigverification.Request

// Empty request batch

type EmptyRequestBatchGenerator struct {
	sliceGenerator     *FastInputArrayGenerator
	batchSizeGenerator *test.PositiveIntGenerator
}

func NewEmptyRequestBatchGenerator(batchSize test.Distribution) *EmptyRequestBatchGenerator {
	valueGen := func() S {
		return &sigverification.Request{}
	}
	return &EmptyRequestBatchGenerator{
		sliceGenerator:     NewFastInputSliceGenerator(valueGen, 40),
		batchSizeGenerator: test.NewPositiveIntGenerator(batchSize, 30),
	}
}

func (c *EmptyRequestBatchGenerator) Next() []S {
	return c.sliceGenerator.NextWithSize(c.batchSizeGenerator.Next())
}

// Code identical to test.FastByteArrayGenerator
type FastInputArrayGenerator struct {
	sample []S
}

func NewFastInputSliceGenerator(valueGenerator func() S, sampleSize int) *FastInputArrayGenerator {
	sample := make([]S, sampleSize)
	for i := 0; i < sampleSize; i++ {
		sample[i] = valueGenerator()
	}
	return &FastInputArrayGenerator{sample: sample}
}

func (g *FastInputArrayGenerator) NextWithSize(targetSize int) []S {
	var batch []S
	for remaining := targetSize; remaining > 0; remaining = targetSize - len(batch) {
		batch = append(batch, g.sample[:utils.Min(len(g.sample), remaining)]...)
	}
	return batch
}

// Tracker

func Track(outputReceived <-chan []*sigverification.Response) (func(int), func() int) {
	requestsSubmitted := make(chan int)
	stopSending := make(chan struct{})
	done := make(chan int)
	totalSent := 0
	start := time.Now()
	go func() {
		pending := 0
		stillSubmitting := true
		for {
			select {
			case <-stopSending:
				stillSubmitting = false
			case inputBatchSize := <-requestsSubmitted:
				pending += inputBatchSize
				totalSent += inputBatchSize
			case outputBatch := <-outputReceived:
				pending -= len(outputBatch)
				if pending < 0 {
					panic("negative pending")
				}
				if pending == 0 && !stillSubmitting {
					done <- requestsPerSecond(totalSent, time.Now().Sub(start))
					return
				}
			}
		}
	}()

	wait := func() int {
		stopSending <- struct{}{}
		outputRate := <-done
		close(stopSending)
		close(requestsSubmitted)
		return outputRate
	}
	submitRequests := func(requests int) {
		requestsSubmitted <- requests
	}

	return submitRequests, wait
}

func requestsPerSecond(total int, duration time.Duration) int {
	return int(float64(time.Second) * float64(total) / float64(duration))
}

// Dummy parallel executor for benchmarking purposes
// For each request, the executor responds with an equally-sized response after the specified delay
type dummyParallelExecutorParams struct {
	executorDelay test.Distribution
}

type dummyParallelExecutor struct {
	outputs                chan []*parallelexecutor.Output
	executorDelayGenerator *test.DelayGenerator
	responseBatchGenerator *FastOutputArrayGenerator
}

func newDummyParallelExecutor(params *dummyParallelExecutorParams) parallelexecutor.ParallelExecutor {
	valueGen := func() *sigverification.Response {
		return &sigverification.Response{}
	}
	return &dummyParallelExecutor{
		outputs:                make(chan []*parallelexecutor.Output),
		executorDelayGenerator: test.NewDelayGenerator(params.executorDelay, 30),
		responseBatchGenerator: NewFastOutputSliceGenerator(valueGen, 10),
	}
}

func (e *dummyParallelExecutor) Submit(input []*parallelexecutor.Input) {
	e.executorDelayGenerator.Next()
	e.outputs <- e.responseBatchGenerator.NextWithSize(len(input))
}
func (e *dummyParallelExecutor) Outputs() <-chan []*parallelexecutor.Output {
	return e.outputs
}
func (e *dummyParallelExecutor) Errors() <-chan error {
	panic("unsupported operation")
}

// Empty verifier server for benchmarking purposes
type dummyVerifierServer struct {
	sigverification.UnimplementedVerifierServer
	streamHandler *streamhandler.StreamHandler
}

func NewDummyVerifierServer(executorDelay test.Distribution) sigverification.VerifierServer {
	executor := newDummyParallelExecutor(&dummyParallelExecutorParams{executorDelay: executorDelay})
	streamHandler := streamhandler.OfExecutor(executor)
	return &dummyVerifierServer{streamHandler: streamHandler}
}

func (s *dummyVerifierServer) SetVerificationKey(context.Context, *sigverification.Key) (*sigverification.Empty, error) {
	return nil, nil
}
func (s *dummyVerifierServer) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	s.streamHandler.HandleStream(stream)
	return nil
}
