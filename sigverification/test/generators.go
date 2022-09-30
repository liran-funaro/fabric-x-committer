package sigverification_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sync/atomic"

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
	TxSigner               TxSigner
	TxInputGenerator       TxInputGenerator
	ValidSigRatioGenerator *test.BooleanGenerator
}

type TxInputGenerator interface {
	Next() []signature.SerialNumber
}

type TxGeneratorParams struct {
	SigningKey       PrivateKey
	Scheme           signature.Scheme
	ValidSigRatio    test.Percentage
	TxSize           test.Distribution
	SerialNumberSize test.Distribution
}

func NewTxGenerator(params *TxGeneratorParams) *TxGenerator {
	txSigner, _ := GetSignatureFactory(params.Scheme).NewSigner(params.SigningKey)
	return &TxGenerator{
		TxSigner:               txSigner,
		TxInputGenerator:       NewSomeTxInputGenerator(&SomeTxInputGeneratorParams{TxSize: params.TxSize, SerialNumberSize: params.SerialNumberSize}),
		ValidSigRatioGenerator: test.NewBooleanGenerator(test.PercentageUniformDistribution, params.ValidSigRatio, 10),
	}
}

func (g *TxGenerator) Next() *token.Tx {
	tx := &token.Tx{SerialNumbers: g.TxInputGenerator.Next()}
	if g.ValidSigRatioGenerator.Next() {
		tx.Signature, _ = g.TxSigner.SignTx(tx.SerialNumbers)
	}
	return tx
}

type SomeTxInputGeneratorParams struct {
	TxSize           test.Distribution
	SerialNumberSize test.Distribution
}

type SomeTxInputGenerator struct {
	serialNumberGenerator *FastTxInputSliceGenerator
	txSizeGenerator       *test.PositiveIntGenerator
}

func NewSomeTxInputGenerator(params *SomeTxInputGeneratorParams) *SomeTxInputGenerator {

	serialNumberSizeGenerator := test.NewPositiveIntGenerator(params.SerialNumberSize, 30)
	serialNumberGenerator := test.NewFastByteArrayGenerator(60)
	txInputValueGenerator := func() signature.SerialNumber {
		return serialNumberGenerator.NextWithSize(serialNumberSizeGenerator.Next())
	}

	return &SomeTxInputGenerator{
		serialNumberGenerator: NewFastTxInputSliceGenerator(txInputValueGenerator, 100),
		txSizeGenerator:       test.NewPositiveIntGenerator(params.TxSize, 50),
	}
}

func (g *SomeTxInputGenerator) Next() []signature.SerialNumber {
	return g.serialNumberGenerator.NextWithSize(g.txSizeGenerator.Next())
}

type LinearTxInputGenerator struct {
	uniqueSerialNum uint64
	Count           int64
}

func (g *LinearTxInputGenerator) Next() []signature.SerialNumber {
	serialNumbers := make([]signature.SerialNumber, g.Count)

	h := sha256.New()
	b := make([]byte, 8)

	for i := int64(0); i < g.Count; i++ {
		binary.LittleEndian.PutUint64(b, atomic.AddUint64(&g.uniqueSerialNum, 1))

		h.Reset()
		h.Write(b)
		sn := h.Sum(nil)

		serialNumbers[i] = sn[:]
	}

	return serialNumbers
}

// TX request batch

type RequestBatchGeneratorParams struct {
	Tx        TxGeneratorParams
	BatchSize test.Distribution
}
type RequestBatchGenerator struct {
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
