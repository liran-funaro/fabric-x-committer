package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/workerpool"
)

type TraceKey interface{}
type KeyHasher func(TraceKey) uint64

var latencyTrackerSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "latency_tracker_size",
	Help: "The size of the latency tracker to make sure it does not get too big",
}, []string{"tracker_name"})

type blockTrace struct {
	sentAt  time.Time
	pending int64
}

type LatencyHistogram struct {
	*prometheus.HistogramVec
	name        string
	trackerSize prometheus.Gauge
	enabled     bool
	traces      map[TraceKey]*blockTrace
	worker      workerpool.WorkerPool
	sampler     SamplingStrategy
}

type SamplingStrategy = func(TraceKey) bool

func SampleThousandPerMillionUsing(hasher KeyHasher) SamplingStrategy {
	const (
		ratio        = 10
		sampleSize   = 1_000
		samplePeriod = 250_000
	)
	return func(key TraceKey) bool {
		hash := hasher(key) % samplePeriod
		return hash < sampleSize && hash%ratio == 0
	}
}

type LatencyHistogramOpts struct {
	Name     string
	Help     string
	Count    int
	From, To time.Duration
	Sampler  SamplingStrategy
	Labels   []string
}

var Identity = func(key TraceKey) uint64 {
	return key.(uint64)
}
var TxSeqNumHasher = func(key TraceKey) uint64 {
	return key.(token.TxSeqNum).BlkNum
}

func NewDefaultLatencyHistogram(name string, maxValue time.Duration, sampler SamplingStrategy, labels ...string) *LatencyHistogram {
	return NewLatencyHistogram(LatencyHistogramOpts{
		Name:    name,
		Help:    "Total latency on the component",
		Count:   1000,
		From:    0,
		To:      maxValue,
		Sampler: sampler,
		Labels:  labels,
	})
}

func NewLatencyHistogram(opts LatencyHistogramOpts) *LatencyHistogram {
	return &LatencyHistogram{
		HistogramVec: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    opts.Name,
			Help:    opts.Help,
			Buckets: UniformBuckets(opts.Count, float64(opts.From), float64(opts.To)),
		}, opts.Labels),
		name:        opts.Name,
		trackerSize: latencyTrackerSize.WithLabelValues(opts.Name),
		traces:      map[TraceKey]*blockTrace{},
		worker: *workerpool.New(&workerpool.Config{
			Parallelism:     1,
			ChannelCapacity: 1000,
		}),
		sampler: opts.Sampler,
	}
}

func (h *LatencyHistogram) Begin(key TraceKey, txCount int, timestamp time.Time) {
	if !h.enabled || !h.sampler(key) {
		return
	}

	h.worker.Run(func(key TraceKey, value *blockTrace) func() {
		return func() {
			h.traces[key] = value
			h.trackerSize.Set(float64(len(h.traces)))
		}
	}(key, &blockTrace{timestamp, int64(txCount)}))
}
func (h *LatencyHistogram) End(key TraceKey, timestamp time.Time, labels ...string) {
	if !h.enabled || !h.sampler(key) {
		return
	}

	h.worker.Run(func(key TraceKey) func() {
		return func() {
			trace, ok := h.traces[key]
			if !ok {
				panic("error with histogram: " + h.name)
			}
			trace.pending--
			if trace.pending == 0 {
				delete(h.traces, key)
			}

			h.HistogramVec.WithLabelValues(labels...).Observe(float64(timestamp.Sub(trace.sentAt)))
		}
	}(key))

}

//SetEnabled is used by the prometheus helper to save some processing power, in case we don't need these metrics
func (h *LatencyHistogram) SetEnabled(enabled bool) {
	h.enabled = enabled
}

//LatencyTrackerSize returns the tracker size metric, that is only for debugging purposes (to make sure the tracker doesn't get too big)
func (h *LatencyHistogram) LatencyTrackerSize() prometheus.Collector {
	return h.trackerSize
}
