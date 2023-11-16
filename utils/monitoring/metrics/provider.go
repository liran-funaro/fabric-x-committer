package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("prometheus provider")

type Provider interface {
	// StartPrometheusServer starts a prometheus server in a separate goroutine
	// and returns an error channel that will receive an error if the server
	// stops unexpectedly.
	StartPrometheusServer() <-chan error

	// StopServer stops the prometheus server.
	StopServer() error

	// URL returns the prometheus server URL.
	URL() string

	// NewCounter creates a new prometheus counter.
	NewCounter(opts prometheus.CounterOpts) prometheus.Counter

	// NewCounterVec creates a new prometheus counter vector.
	NewCounterVec(opts prometheus.CounterOpts, labels []string) *prometheus.CounterVec

	// NewGauge creates a new prometheus gauge.
	NewGauge(opts prometheus.GaugeOpts) prometheus.Gauge

	// NewGaugeVec creates a new prometheus gauge vector.
	NewGaugeVec(opts prometheus.GaugeOpts, labels []string) *prometheus.GaugeVec

	// NewHistogram creates a new prometheus histogram.
	NewHistogram(opts prometheus.HistogramOpts) prometheus.Histogram

	// NewHistogramVec creates a new prometheus histogram vector.
	NewHistogramVec(opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec

	TxSampler() TxTracingSampler

	BatchSampler() BatchTracingSampler

	BlockSampler() BlockTracingSampler

	Buckets() []float64
}
