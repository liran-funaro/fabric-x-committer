package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/workerpool"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type traceData struct {
	start time.Time
	span  trace.Span
}

type latencyTracer struct {
	histogram *prometheus.HistogramVec
	name      string
	enabled   bool
	traces    map[token.TxSeqNum]*traceData
	worker    workerpool.WorkerPool
	sampler   TxSeqNumSampler
	tracer    trace.Tracer
	labels    []string
}

type TxSeqNumSampler = func(key token.TxSeqNum) bool
type LatencyTracerOpts struct {
	Name     string
	Help     string
	Count    int
	From, To time.Duration
	Sampler  TxSeqNumSampler
	Labels   []string
}

const (
	ratio        = 10
	sampleSize   = 1_000
	samplePeriod = 250_000
)

var AlwaysSampler = func(key token.TxSeqNum) bool {
	return true
}

func OnceSampler(uniqueKey token.TxSeqNum) TxSeqNumSampler {
	return func(key token.TxSeqNum) bool {
		return uniqueKey == key
	}
}

var ScarceSampler = func(key token.TxSeqNum) bool {
	hash := key.BlkNum % samplePeriod
	return hash < sampleSize && hash%ratio == 0
}

func NewDefaultLatencyTracer(name string, maxValue time.Duration, tp *sdktrace.TracerProvider, labels ...string) *latencyTracer {
	return NewLatencyTracer(LatencyTracerOpts{
		Name:    name,
		Help:    "Total latency on the component",
		Count:   1000,
		From:    0,
		To:      maxValue,
		Sampler: ScarceSampler,
		Labels:  labels,
	}, tp)
}

type NoopLatencyTracer struct{}

func (t *NoopLatencyTracer) Start(token.TxSeqNum)                         {}
func (t *NoopLatencyTracer) StartAt(token.TxSeqNum, time.Time)            {}
func (t *NoopLatencyTracer) AddEvent(token.TxSeqNum, string)              {}
func (t *NoopLatencyTracer) AddEventAt(token.TxSeqNum, string, time.Time) {}
func (t *NoopLatencyTracer) End(token.TxSeqNum, ...string)                {}
func (t *NoopLatencyTracer) EndAt(token.TxSeqNum, time.Time, ...string)   {}
func (t *NoopLatencyTracer) Collectors() []prometheus.Collector {
	return []prometheus.Collector{}
}

type AppTracer interface {
	Start(token.TxSeqNum)
	StartAt(token.TxSeqNum, time.Time)
	AddEvent(token.TxSeqNum, string)
	AddEventAt(token.TxSeqNum, string, time.Time)
	End(token.TxSeqNum, ...string)
	EndAt(token.TxSeqNum, time.Time, ...string)
	Collectors() []prometheus.Collector
}

func NewLatencyTracer(opts LatencyTracerOpts, tp *sdktrace.TracerProvider) *latencyTracer {
	return &latencyTracer{
		enabled: true,
		histogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    opts.Name,
			Help:    opts.Help,
			Buckets: UniformBuckets(opts.Count, float64(opts.From), float64(opts.To)),
		}, opts.Labels),
		name:   opts.Name,
		tracer: tp.Tracer(opts.Name),
		traces: map[token.TxSeqNum]*traceData{},
		worker: *workerpool.New(&workerpool.Config{
			Parallelism:     1,
			ChannelCapacity: 1000,
		}),
		sampler: opts.Sampler,
		labels:  opts.Labels,
	}
}

func (h *latencyTracer) Start(key token.TxSeqNum) {
	h.StartAt(key, time.Now())
}

func (h *latencyTracer) StartAt(key token.TxSeqNum, timestamp time.Time) {
	if !h.enabled || !h.sampler(key) {
		return
	}

	h.worker.Run(func(key token.TxSeqNum, timestamp time.Time) func() {
		return func() {
			ctx := context.WithValue(context.Background(), "id", key.String())
			_, span := h.tracer.Start(ctx, "TxRequest", trace.WithTimestamp(timestamp), trace.WithAttributes(attribute.String("id", key.String())))
			h.traces[key] = &traceData{timestamp, span}
		}
	}(key, timestamp))
}

func (h *latencyTracer) AddEvent(key token.TxSeqNum, name string) {
	h.AddEventAt(key, name, time.Now())
}

func (h *latencyTracer) AddEventAt(key token.TxSeqNum, name string, timestamp time.Time) {
	if !h.enabled || !h.sampler(key) {
		return
	}

	h.worker.Run(func(key token.TxSeqNum, timestamp time.Time) func() {
		return func() {
			t, ok := h.traces[key]
			if !ok {
				panic("error with tracer: " + h.name + " at event: " + name)
			}

			t.span.AddEvent(name, trace.WithTimestamp(timestamp))
		}
	}(key, timestamp))
}

func (h *latencyTracer) End(key token.TxSeqNum, labels ...string) {
	h.EndAt(key, time.Now(), labels...)
}

func (h *latencyTracer) EndAt(key token.TxSeqNum, timestamp time.Time, labels ...string) {
	if !h.enabled || !h.sampler(key) {
		return
	}

	attributes := make([]attribute.KeyValue, len(h.labels))
	for i, label := range h.labels {
		attributes[i] = attribute.String(label, labels[i])
	}

	h.worker.Run(func(key token.TxSeqNum, timestamp time.Time) func() {
		return func() {
			t, ok := h.traces[key]
			if !ok {
				panic("error with tracer: " + h.name + " at end")
			}
			t.span.SetAttributes(attributes...)
			t.span.End(trace.WithTimestamp(timestamp))
			delete(h.traces, key)
			h.histogram.WithLabelValues(labels...).Observe(float64(timestamp.Sub(t.start)))
		}
	}(key, timestamp))

}

func (t *latencyTracer) Collectors() []prometheus.Collector {
	return []prometheus.Collector{t.histogram}
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
