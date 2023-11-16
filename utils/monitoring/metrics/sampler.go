package metrics

import (
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/token"
)

type TxTracingSampler = func(key TxTracingId) bool
type TxTracingId = string
type BlockTracingSampler = func(blockNumber uint64) bool
type BatchTracingSampler = func() bool

type TracerConfig struct {
	Name   string
	Labels []string
}
type TraceIdSamplerType string

const (
	Always   TraceIdSamplerType = "always"
	Never                       = "never"
	Prefixed                    = "prefixed"
	BlockTx                     = "blocktx"
	Timer                       = "timer"
)

type SamplerProvider interface {
	TxSampler() TxTracingSampler
	BatchSampler() BatchTracingSampler
	BlockSampler() BlockTracingSampler
}

type SamplerConfig struct {
	Type TraceIdSamplerType `mapstructure:"type"`
	// Prefix related
	Prefix string `mapstructure:"prefix"`
	// BlockTx related
	SamplePeriod     uint64        `mapstructure:"sample-period"`
	SampleSize       uint64        `mapstructure:"sample-size"`
	Ratio            uint64        `mapstructure:"ratio"`
	SamplingInterval time.Duration `mapstructure:"sampling-interval"`
}

func (c *SamplerConfig) TxSampler() TxTracingSampler {
	switch c.Type {
	case Always:
		return func(id TxTracingId) bool { return true }
	case Never:
		return func(id TxTracingId) bool { return false }
	case "":
		return func(id TxTracingId) bool { return false }
	case Prefixed:
		prefixLength := len(c.Prefix)
		return func(id TxTracingId) bool {
			str := id
			return len(str) >= prefixLength && str[:prefixLength] == c.Prefix
		}
	case BlockTx:
		return func(key TxTracingId) bool {
			hash := token.TxSeqNumFromString(key).BlkNum % c.SamplePeriod
			return hash < c.SampleSize && hash%c.Ratio == 0
		}
	case Timer:
		ticker := time.NewTicker(c.SamplingInterval)
		return func(TxTracingId) bool {
			select {
			case <-ticker.C:
				return true
			default:
				return false
			}
		}
	default:
		panic("type " + c.Type + " not defined")
	}
}

func (c *SamplerConfig) BatchSampler() BatchTracingSampler {
	switch c.Type {
	case Always:
		return func() bool { return true }
	case Never:
		return func() bool { return false }
	case "":
		return func() bool { return false }
	case Timer:
		ticker := time.NewTicker(c.SamplingInterval)
		return func() bool {
			select {
			case <-ticker.C:
				return true
			default:
				return false
			}
		}
	default:
		panic("type " + c.Type + " not supported")
	}
}

func (c *SamplerConfig) BlockSampler() BlockTracingSampler {
	switch c.Type {
	case Always:
		return func(uint64) bool { return true }
	case Never:
		return func(uint64) bool { return false }
	case "":
		return func(uint64) bool { return false }
	case Timer:
		ticker := time.NewTicker(c.SamplingInterval)
		return func(uint64) bool {
			select {
			case <-ticker.C:
				return true
			default:
				return false
			}
		}
	default:
		panic("type " + c.Type + " not supported")
	}
}
