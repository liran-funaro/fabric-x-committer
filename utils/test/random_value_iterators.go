package test

import "github.ibm.com/distributed-trust-research/scalable-committer/utils"

type PositiveIntGenerator struct {
	items []int
	idx   *cyclicIndexIterator
}

func NewPositiveIntGenerator(distribution Distribution, precalculatedItems int) *PositiveIntGenerator {
	items := make([]int, precalculatedItems)
	for i := 0; i < precalculatedItems; i++ {
		items[i] = utils.Max(int(distribution.Generate()), 0)
	}
	return &PositiveIntGenerator{items: items, idx: &cyclicIndexIterator{size: len(items)}}
}

func (i *PositiveIntGenerator) Next() int {
	return i.items[i.idx.NextIndex()]
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
	return i.items[i.idx.NextIndex()]
}

type cyclicIndexIterator struct {
	idx  int
	size int
}

func (c *cyclicIndexIterator) NextIndex() int {
	next := c.idx
	c.idx = (c.idx + 1) % c.size
	return next
}

type S = byte

var defaultValue S = 1

type FastByteArrayGenerator struct {
	sample []S
}

func NewFastByteArrayGenerator(sampleSize int) *FastByteArrayGenerator {
	sample := make([]S, sampleSize)
	for i := 0; i < sampleSize; i++ {
		sample[i] = defaultValue
	}
	return &FastByteArrayGenerator{sample: sample}
}

func (g *FastByteArrayGenerator) Next(targetSize int) []S {
	var batch []S
	for remaining := targetSize; remaining > 0; remaining = targetSize - len(batch) {
		batch = append(batch, g.sample[:utils.Min(len(g.sample), remaining)]...)
	}
	return batch
}
