package latency

import (
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/protos/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type SpanExporterType string

const (
	Jaeger  SpanExporterType = "jaeger"
	Console                  = "console"
)

type Config struct {
	SpanExporter SpanExporterType     `mapstructure:"span-exporter"`
	Sampler      TraceIdSamplerConfig `mapstructure:"sampler"`
	Endpoint     *connection.Endpoint `mapstructure:"endpoint"`
	MaxLatency   time.Duration        `mapstructure:"max-latency"`
	BucketCount  int                  `mapstructure:"bucket-count"`
}
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
)

type TraceIdSamplerConfig struct {
	Type TraceIdSamplerType `mapstructure:"type"`
	// Prefix related
	Prefix string `mapstructure:"prefix"`
	// BlockTx related
	SamplePeriod uint64 `mapstructure:"sample-period"`
	SampleSize   uint64 `mapstructure:"sample-size"`
	Ratio        uint64 `mapstructure:"ratio"`
}

func (c *TraceIdSamplerConfig) Sampler() func(TxTracingId) bool {
	switch c.Type {
	case Always:
		return func(id TxTracingId) bool { return true }
	case Never:
		return func(id TxTracingId) bool { return false }
	case Prefixed:
		prefixLength := len(c.Prefix)
		return func(id TxTracingId) bool {
			str := id.String()
			return len(str) >= prefixLength && str[:prefixLength] == c.Prefix
		}
	case BlockTx:
		return func(key TxTracingId) bool {
			hash := key.(token.TxSeqNum).BlkNum % c.SamplePeriod
			return hash < c.SampleSize && hash%c.Ratio == 0
		}
	default:
		panic("type " + c.Type + " not defined")
	}
}
