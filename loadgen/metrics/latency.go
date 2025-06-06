/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package metrics

import (
	"encoding/binary"
	"sort"
	"time"

	"github.com/cockroachdb/errors"
)

type (
	// LatencyConfig describes the latency monitoring parameters.
	LatencyConfig struct {
		SamplerConfig SamplerConfig `mapstructure:"sampler"`
		BucketConfig  BucketConfig  `mapstructure:"buckets"`
	}

	// SamplerConfig describes the latency sampling parameters.
	SamplerConfig struct {
		Type   TraceSamplerType `mapstructure:"type"`
		Always bool             `mapstructure:"always"`
		Never  bool             `mapstructure:"never"`
		Prefix string           `mapstructure:"prefix"`
		Hash   *SampleHash      `mapstructure:"hash"`
	}

	// SampleHash hash sampler.
	SampleHash struct {
		SamplePeriod uint64 `mapstructure:"sample-period"`
		SampleSize   uint64 `mapstructure:"sample-size"`
		Ratio        uint64 `mapstructure:"ratio"`
	}

	// BucketConfig describes the latency bucket distribution.
	BucketConfig struct {
		Distribution BucketDistribution `mapstructure:"distribution"`
		MaxLatency   time.Duration      `mapstructure:"max-latency"`
		BucketCount  int                `mapstructure:"bucket-count"`
		Values       []float64          `mapstructure:"values"`
	}

	// BucketDistribution can be empty, uniform, or fixed.
	BucketDistribution string
	// TraceSamplerType can be always, never, prefix, hash, or timer.
	TraceSamplerType string

	// KeyTracingSampler returns true to sample a given key.
	KeyTracingSampler = func(key string) bool
	// NumberTracingSampler returns true to sample a given number.
	NumberTracingSampler = func(blockNumber uint64) bool
)

// Sample and bucket constants.
const (
	BucketEmpty   BucketDistribution = "empty"
	BucketUniform BucketDistribution = "uniform"
	BucketFixed   BucketDistribution = "fixed"
)

// Buckets returns a list of buckets for latency monitoring.
func (c *BucketConfig) Buckets() []float64 {
	switch c.Distribution {
	case BucketUniform:
		required(c.BucketCount >= 0, "invalid bucket count")
		required(c.MaxLatency > 0, "invalid max latency")
		return uniformBuckets(c.BucketCount, 0, c.MaxLatency.Seconds())
	case BucketFixed:
		required(len(c.Values) > 0, "no fixed values provided")
		sort.Float64s(c.Values)
		return c.Values
	case BucketEmpty:
		return []float64{}
	case "":
		return []float64{}
	default:
		panic("undefined distribution: " + c.Distribution)
	}
}

// TxSampler returns a KeyTracingSampler for latency monitoring.
func (c *SamplerConfig) TxSampler() KeyTracingSampler {
	switch {
	default:
		fallthrough
	case c.Never:
		return func(string) bool { return false }
	case c.Always:
		return func(string) bool { return true }
	case len(c.Prefix) > 0:
		return func(key string) bool {
			return len(key) >= len(c.Prefix) && key[:len(c.Prefix)] == c.Prefix
		}
	case c.Hash != nil:
		return func(key string) bool {
			hash := binary.LittleEndian.Uint64([]byte(key))
			return hash%c.Hash.SamplePeriod < c.Hash.SampleSize && hash%c.Hash.Ratio == 0
		}
	}
}

func uniformBuckets(count int, from, to float64) []float64 {
	if to < from {
		panic("invalid input")
	}
	result := make([]float64, 0, count)
	step := (to - from) / float64(count-1)
	for low := from; low < to; low += step {
		result = append(result, low)
	}
	return append(result, to)
}

//nolint:revive // flag-parameter: parameter 'condition' seems to be a control flag, avoid control coupling.
func required(condition bool, msg string) {
	if !condition {
		panic(errors.New(msg))
	}
}
