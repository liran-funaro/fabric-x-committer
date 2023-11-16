package metrics

import (
	"sort"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type Config struct {
	Enable   bool                 `mapstructure:"enable"`
	Endpoint *connection.Endpoint `mapstructure:"endpoint"`
	Latency  LatencyConfig        `mapstructure:"latency"`
}

type LatencyConfig struct {
	SamplerConfig SamplerConfig `mapstructure:"sampler"`
	BucketConfig  BucketConfig  `mapstructure:"buckets"`
}

func (c *LatencyConfig) Buckets() []float64 {
	return c.BucketConfig.Buckets()
}

func (c *LatencyConfig) TxSampler() TxTracingSampler {
	return c.SamplerConfig.TxSampler()
}

func (c *LatencyConfig) BatchSampler() BatchTracingSampler {
	return c.SamplerConfig.BatchSampler()
}

func (c *LatencyConfig) BlockSampler() BlockTracingSampler {
	return c.SamplerConfig.BlockSampler()
}

type BucketType string

const (
	Uniform BucketType = "uniform"
	Empty   BucketType = "empty"
	Fixed   BucketType = "fixed"
)

type BucketConfig struct {
	Type        BucketType    `mapstructure:"type"`
	MaxLatency  time.Duration `mapstructure:"max-latency"`
	BucketCount int           `mapstructure:"bucket-count"`
	Values      []float64     `mapstructure:"values"`
}

func (c *BucketConfig) Buckets() []float64 {
	switch c.Type {
	case Uniform:
		utils.Require(c.BucketCount >= 0, "invalid bucket count")
		utils.Require(c.MaxLatency > 0, "invalid max latency")
		return utils.UniformBuckets(c.BucketCount, 0, c.MaxLatency.Seconds())
	case Fixed:
		utils.Require(len(c.Values) > 0, "no fixed values provided")
		sort.Float64s(c.Values)
		return c.Values
	case Empty:
		return []float64{}
	case "":
		return []float64{}
	default:
		panic("undefined type: " + c.Type)
	}
}
