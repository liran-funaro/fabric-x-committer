package test

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"time"
)

type PositiveIntGenerator struct {
	items []int
	idx   *cyclicIndexIterator
}

func NewPositiveIntGenerator(distribution Distribution, precalculatedItems int) *PositiveIntGenerator {
	items := make([]int, precalculatedItems)
	for i := 0; i < precalculatedItems; i++ {
		items[i] = utils.Max(int(distribution.Generate()), 1)
	}
	return &PositiveIntGenerator{items: items, idx: &cyclicIndexIterator{size: len(items)}}
}

func (i *PositiveIntGenerator) Next() int {
	return i.items[i.idx.Next()]
}

type BooleanGenerator struct {
	items []bool
	idx   *cyclicIndexIterator
}

func NewBooleanGenerator(distribution Distribution, probability Percentage, precalculatedItems int) *BooleanGenerator {
	items := make([]bool, precalculatedItems)
	for i := 0; i < precalculatedItems; i++ {
		items[i] = distribution.Generate() < probability
	}
	return &BooleanGenerator{items: items, idx: &cyclicIndexIterator{size: len(items)}}
}

func (i *BooleanGenerator) Next() bool {
	return i.items[i.idx.Next()]
}

type DelayGenerator struct {
	delay *PositiveIntGenerator
}

func NewDelayGenerator(delay Distribution, precalculatedItems int) *DelayGenerator {
	return &DelayGenerator{
		delay: NewPositiveIntGenerator(delay, precalculatedItems),
	}
}

func (g *DelayGenerator) Next() {
	delay := time.Duration(g.delay.Next())
	busyWait(delay)
}

func busyWait(delay time.Duration) {
	end := time.Now().Add(delay)
	for {
		if time.Now().After(end) {
			return
		}
	}
}

type cyclicIndexIterator struct {
	idx  int
	size int
}

func (c *cyclicIndexIterator) Next() int {
	next := c.idx
	c.idx = (c.idx + 1) % c.size
	return next
}

type S = byte

var ConstantByteGenerator = func() S {
	return 1
}

//FastByteArrayGenerator creates slices of S of any size.
//For faster results, it pre-calculates a sample and when a new slice is requested,
//it is calculated by concatenating the sample the necessary times to achieve the target size.
//The size of the sample is a tradeoff between the randomness of the values of the generated array and the memory footprint.
type FastByteArrayGenerator struct {
	sample             []S
	batchSizeGenerator *PositiveIntGenerator
}

func NewFastByteArrayGenerator(valueGenerator func() S, batchSize Distribution, sampleSize int) *FastByteArrayGenerator {
	sample := make([]S, sampleSize)
	for i := 0; i < sampleSize; i++ {
		sample[i] = valueGenerator()
	}
	return &FastByteArrayGenerator{
		sample:             sample,
		batchSizeGenerator: NewPositiveIntGenerator(batchSize, 30),
	}
}

func (g *FastByteArrayGenerator) Next() []S {
	return g.NextWithSize(g.batchSizeGenerator.Next())
}

func (g *FastByteArrayGenerator) NextWithSize(targetSize int) []S {
	var batch []S
	for remaining := targetSize; remaining > 0; remaining = targetSize - len(batch) {
		batch = append(batch, g.sample[:utils.Min(len(g.sample), remaining)]...)
	}
	return batch
}
