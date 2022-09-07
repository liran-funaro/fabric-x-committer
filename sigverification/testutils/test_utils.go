package testutils

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"time"
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
		signTx:                 signTx,
		PublicKey:              verificationKey,
		txSizeGenerator:        test.NewPositiveIntGenerator(params.TxSize, 50),
		serialNumberGenerator:  test.NewFastByteArrayGenerator(test.ConstantByteGenerator, params.SerialNumberSize, 60),
		validSigRatioGenerator: test.NewBooleanGenerator(test.PercentageUniformDistribution, params.ValidSigRatio, 10),
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
		serialNumbers[i] = g.serialNumberGenerator.Next()
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
		inputArrayGenerator: NewFastInputSliceGenerator(valueGen, params.BatchSize, sampleSize),
	}
}

func (c *RequestBatchGenerator) Next() []*sigverification.Request {
	return c.inputArrayGenerator.Next()
}

// Empty request batch

type EmptyRequestBatchGenerator struct {
	inputArrayGenerator *FastInputArrayGenerator
}

func NewEmptyRequestBatchGenerator(batchSize test.Distribution) *EmptyRequestBatchGenerator {
	valueGen := func() *sigverification.Request {
		return &sigverification.Request{}
	}
	return &EmptyRequestBatchGenerator{
		inputArrayGenerator: NewFastInputSliceGenerator(valueGen, batchSize, 40),
	}
}

func (c *EmptyRequestBatchGenerator) Next() []*sigverification.Request {
	return c.inputArrayGenerator.Next()
}

type S = *sigverification.Request

// Code identical to test.FastByteArrayGenerator
type FastInputArrayGenerator struct {
	sample             []S
	batchSizeGenerator *test.PositiveIntGenerator
}

func NewFastInputSliceGenerator(valueGenerator func() S, batchSize test.Distribution, sampleSize int) *FastInputArrayGenerator {
	sample := make([]S, sampleSize)
	for i := 0; i < sampleSize; i++ {
		sample[i] = valueGenerator()
	}
	return &FastInputArrayGenerator{
		sample:             sample,
		batchSizeGenerator: test.NewPositiveIntGenerator(batchSize, 30),
	}
}

func (g *FastInputArrayGenerator) Next() []S {
	return g.NextWithSize(g.batchSizeGenerator.Next())
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
