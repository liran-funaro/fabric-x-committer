package latency

import (
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
)

type SpanExporterType string

const (
	Jaeger  SpanExporterType = "jaeger"
	Console                  = "console"
)

type Config struct {
	SpanExporter SpanExporterType      `mapstructure:"span-exporter"`
	Sampler      metrics.SamplerConfig `mapstructure:"sampler"`
	Endpoint     *connection.Endpoint  `mapstructure:"endpoint"`
	MaxLatency   time.Duration         `mapstructure:"max-latency"`
	BucketCount  int                   `mapstructure:"bucket-count"`
}
