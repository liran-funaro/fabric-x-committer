package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/workerpool"
)

type TraceKey interface{}

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
	trackerSize prometheus.Gauge
	enabled     bool
	traces      map[TraceKey]*blockTrace
	worker      workerpool.WorkerPool
}

type LatencyHistogramOpts struct {
	Name     string
	Help     string
	Count    int
	From, To time.Duration
	Labels   []string
}

func NewLatencyHistogram(opts LatencyHistogramOpts) *LatencyHistogram {
	return &LatencyHistogram{
		HistogramVec: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    opts.Name,
			Help:    opts.Help,
			Buckets: UniformBuckets(opts.Count, float64(opts.From), float64(opts.To)),
		}, opts.Labels),
		trackerSize: latencyTrackerSize.WithLabelValues(opts.Name),
		traces:      map[TraceKey]*blockTrace{},
		worker: *workerpool.New(&workerpool.Config{
			Parallelism:     1,
			ChannelCapacity: 10,
		}),
	}
}

func UniformBuckets(count int, from, to float64) []float64 {
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

func (h *LatencyHistogram) Begin(key TraceKey, txCount int, timestamp time.Time) {
	if !h.enabled {
		return
	}
	var size int
	h.worker.Run(func(key TraceKey, value *blockTrace, size *int) func() {
		return func() {
			h.traces[key] = value
			*size = len(h.traces)
		}
	}(key, &blockTrace{timestamp, int64(txCount)}, &size))
	h.trackerSize.Set(float64(size))
}
func (h *LatencyHistogram) End(key TraceKey, timestamp time.Time, labels ...string) {
	if !h.enabled {
		return
	}

	var trace blockTrace
	h.worker.Run(func(key TraceKey, trace *blockTrace) func() {
		return func() {
			*trace = *h.traces[key]
			trace.pending--
			if trace.pending == 0 {
				delete(h.traces, key)
			}
		}
	}(key, &trace))

	h.HistogramVec.WithLabelValues(labels...).Observe(float64(timestamp.Sub(trace.sentAt)))
}

//SetEnabled is used by the prometheus helper to save some processing power, in case we don't need these metrics
func (h *LatencyHistogram) SetEnabled(enabled bool) {
	h.enabled = enabled
}

//LatencyTrackerSize returns the tracker size metric, that is only for debugging purposes (to make sure the tracker doesn't get too big)
func (h *LatencyHistogram) LatencyTrackerSize() prometheus.Collector {
	return h.trackerSize
}
