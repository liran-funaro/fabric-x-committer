package sigverification_test

import (
	"context"

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
	txSigner  TxSigner
	PublicKey signature.PublicKey

	txInputGenerator       *TxInputGenerator
	validSigRatioGenerator *test.BooleanGenerator
}

type TxGeneratorParams struct {
	Scheme           signature.Scheme
	ValidSigRatio    test.Percentage
	TxSize           test.Distribution
	SerialNumberSize test.Distribution
}

func NewTxGenerator(params *TxGeneratorParams) *TxGenerator {
	factory := GetSignatureFactory(params.Scheme)
	privateKey, publicKey := factory.NewKeys()
	txSigner, _ := factory.NewSigner(privateKey)
	return &TxGenerator{
		txSigner:               txSigner,
		PublicKey:              publicKey,
		txInputGenerator:       NewTxInputGenerator(&TxInputGeneratorParams{TxSize: params.TxSize, SerialNumberSize: params.SerialNumberSize}),
		validSigRatioGenerator: test.NewBooleanGenerator(test.PercentageUniformDistribution, params.ValidSigRatio, 10),
	}
}

func (g *TxGenerator) Next() *token.Tx {
	tx := &token.Tx{SerialNumbers: g.txInputGenerator.Next()}
	if g.validSigRatioGenerator.Next() {
		tx.Signature, _ = g.txSigner.SignTx(tx.SerialNumbers)
	}
	return tx
}

type TxInputGeneratorParams struct {
	TxSize           test.Distribution
	SerialNumberSize test.Distribution
}

type TxInputGenerator struct {
	serialNumberGenerator *FastTxInputSliceGenerator
	txSizeGenerator       *test.PositiveIntGenerator
}

func NewTxInputGenerator(params *TxInputGeneratorParams) *TxInputGenerator {

	serialNumberSizeGenerator := test.NewPositiveIntGenerator(params.SerialNumberSize, 30)
	serialNumberGenerator := test.NewFastByteArrayGenerator(60)
	txInputValueGenerator := func() signature.SerialNumber {
		return serialNumberGenerator.NextWithSize(serialNumberSizeGenerator.Next())
	}

	return &TxInputGenerator{
		serialNumberGenerator: NewFastTxInputSliceGenerator(txInputValueGenerator, 100),
		txSizeGenerator:       test.NewPositiveIntGenerator(params.TxSize, 50),
	}
}

func (g *TxInputGenerator) Next() []signature.SerialNumber {
	return g.serialNumberGenerator.NextWithSize(g.txSizeGenerator.Next())
}

// TX request batch

type RequestBatchGeneratorParams struct {
	Tx        TxGeneratorParams
	BatchSize test.Distribution
}
type RequestBatchGenerator struct {
	signature.PublicKey
	inputArrayGenerator *FastInputArrayGenerator
	batchSizeGenerator  *test.PositiveIntGenerator
}

func NewRequestBatchGenerator(params *RequestBatchGeneratorParams, sampleSize int) *RequestBatchGenerator {
	txGenerator := NewTxGenerator(&params.Tx)
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

type R = signature.SerialNumber

// Code identical to test.FastByteArrayGenerator
type FastTxInputSliceGenerator struct {
	sample []R
}

func NewFastTxInputSliceGenerator(valueGenerator func() R, sampleSize int) *FastTxInputSliceGenerator {
	sample := make([]R, sampleSize)
	for i := 0; i < sampleSize; i++ {
		sample[i] = valueGenerator()
	}
	return &FastTxInputSliceGenerator{sample: sample}
}

func (g *FastTxInputSliceGenerator) NextWithSize(targetSize int) []R {
	var batch []R
	for remaining := targetSize; remaining > 0; remaining = targetSize - len(batch) {
		batch = append(batch, g.sample[:utils.Min(len(g.sample), remaining)]...)
	}
	return batch
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
	responseGenerator := func() *sigverification.Response { return &sigverification.Response{} }
	return &dummyParallelExecutor{
		outputs:                make(chan []*parallelexecutor.Output),
		executorDelayGenerator: test.NewDelayGenerator(params.executorDelay, 30),
		responseBatchGenerator: NewFastOutputSliceGenerator(responseGenerator, 200),
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
	streamHandler := streamhandler.New(
		func(batch *sigverification.RequestBatch) {
			executor.Submit(batch.Requests)
		},
		func() streamhandler.Output {
			return &sigverification.ResponseBatch{Responses: <-executor.Outputs()}
		})
	return &dummyVerifierServer{streamHandler: streamHandler}
}

func (s *dummyVerifierServer) SetVerificationKey(context.Context, *sigverification.Key) (*sigverification.Empty, error) {
	return nil, nil
}
func (s *dummyVerifierServer) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	s.streamHandler.HandleStream(stream)
	return nil
}

type InputGeneratorParams struct {
	InputDelay   test.Distribution
	RequestBatch RequestBatchGeneratorParams
}
type InputGenerator struct {
	inputDelayGenerator   *test.DelayGenerator
	requestBatchGenerator *RequestBatchGenerator
}

func NewInputGenerator(p *InputGeneratorParams) *InputGenerator {
	return &InputGenerator{
		inputDelayGenerator:   test.NewDelayGenerator(p.InputDelay, 30),
		requestBatchGenerator: NewRequestBatchGenerator(&p.RequestBatch, 30),
	}
}

func (c *InputGenerator) NextRequestBatch() *sigverification.RequestBatch {
	c.inputDelayGenerator.Next()
	return &sigverification.RequestBatch{Requests: c.requestBatchGenerator.Next()}
}

func (c *InputGenerator) PublicKey() *sigverification.Key {
	return &sigverification.Key{SerializedBytes: c.requestBatchGenerator.PublicKey}
}
