package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/workerpool"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var logger = logging.New("latencytracer")

type traceData struct {
	start time.Time
	span  trace.Span
}

type TxTracingId = interface {
	String() string
}

type latencyTracer struct {
	histogram    *prometheus.HistogramVec
	name         string
	enabled      bool
	traces       map[TxTracingId]*traceData
	errorHandler func(interface{})
	worker       workerpool.WorkerPool
	sampler      TxTracingSampler
	tracer       trace.Tracer
	labels       []string
}

type TxTracingSampler = func(key TxTracingId) bool
type LatencyTracerOpts struct {
	Name           string
	Help           string
	Count          int
	From, To       time.Duration
	Sampler        TxTracingSampler
	IgnoreNotFound bool
	Labels         []string
}

const (
	ratio        = 10
	sampleSize   = 100
	samplePeriod = 100_000
)

var AlwaysSampler = func(key TxTracingId) bool {
	return true
}

func OnceSampler(uniqueKey TxTracingId) TxTracingSampler {
	return func(key TxTracingId) bool {
		return uniqueKey == key
	}
}

var ScarceSampler = func(key TxTracingId) bool {
	hash := key.(token.TxSeqNum).BlkNum % samplePeriod
	return hash < sampleSize && hash%ratio == 0
}

func NewPrefixSampler(prefix string) TxTracingSampler {
	prefixLength := len(prefix)
	return func(id TxTracingId) bool {
		str := id.String()
		return len(str) >= prefixLength && str[:prefixLength] == prefix
	}
}

func NewDefaultLatencyTracer(name string, maxValue time.Duration, tp *sdktrace.TracerProvider, labels ...string) *latencyTracer {
	return NewLatencyTracer(LatencyTracerOpts{
		Name:           name,
		Help:           "Total latency on the component",
		Count:          1000,
		From:           0,
		To:             maxValue,
		Sampler:        ScarceSampler,
		IgnoreNotFound: true,
		Labels:         labels,
	}, tp)
}

type NoopLatencyTracer struct{}

func (t *NoopLatencyTracer) Start(TxTracingId)                         {}
func (t *NoopLatencyTracer) StartAt(TxTracingId, time.Time)            {}
func (t *NoopLatencyTracer) AddEvent(TxTracingId, string)              {}
func (t *NoopLatencyTracer) AddEventAt(TxTracingId, string, time.Time) {}
func (t *NoopLatencyTracer) End(TxTracingId, ...string)                {}
func (t *NoopLatencyTracer) EndAt(TxTracingId, time.Time, ...string)   {}
func (t *NoopLatencyTracer) Collectors() []prometheus.Collector {
	return []prometheus.Collector{}
}

type AppTracer interface {
	Start(TxTracingId)
	StartAt(TxTracingId, time.Time)
	AddEvent(TxTracingId, string)
	AddEventAt(TxTracingId, string, time.Time)
	End(TxTracingId, ...string)
	EndAt(TxTracingId, time.Time, ...string)
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
		traces: map[TxTracingId]*traceData{},
		worker: *workerpool.New(&workerpool.Config{
			Parallelism:     1,
			ChannelCapacity: 1000,
		}),
		sampler:      opts.Sampler,
		errorHandler: newErrorHandler(opts.IgnoreNotFound),
		labels:       opts.Labels,
	}
}

func newErrorHandler(ignore bool) func(interface{}) {
	if ignore {
		return func(s interface{}) {
			logger.Debug(s)
		}
	} else {
		return func(s interface{}) {
			panic(s)
		}
	}
}

func (h *latencyTracer) Start(key TxTracingId) {
	h.StartAt(key, time.Now())
}

func (h *latencyTracer) StartAt(key TxTracingId, timestamp time.Time) {
	if !h.enabled || !h.sampler(key) {
		return
	}

	h.worker.Run(func(key TxTracingId, timestamp time.Time) func() {
		return func() {
			ctx := context.WithValue(context.Background(), "id", key.String())
			_, span := h.tracer.Start(ctx, "TxRequest", trace.WithTimestamp(timestamp), trace.WithAttributes(attribute.String("id", key.String())))
			h.traces[key] = &traceData{timestamp, span}
		}
	}(key, timestamp))
}

func (h *latencyTracer) AddEvent(key TxTracingId, name string) {
	h.AddEventAt(key, name, time.Now())
}

func (h *latencyTracer) AddEventAt(key TxTracingId, name string, timestamp time.Time) {
	if !h.enabled || !h.sampler(key) {
		return
	}

	h.worker.Run(func(key TxTracingId, timestamp time.Time) func() {
		return func() {
			t, ok := h.traces[key]
			if !ok {
				h.errorHandler(fmt.Sprintf("error with tracer: %s at event: %s", h.name, name))
			} else {
				t.span.AddEvent(name, trace.WithTimestamp(timestamp))
			}
		}
	}(key, timestamp))
}

func (h *latencyTracer) End(key TxTracingId, labels ...string) {
	h.EndAt(key, time.Now(), labels...)
}

func (h *latencyTracer) EndAt(key TxTracingId, timestamp time.Time, labels ...string) {
	if !h.enabled || !h.sampler(key) {
		return
	}

	attributes := make([]attribute.KeyValue, len(h.labels))
	for i, label := range h.labels {
		attributes[i] = attribute.String(label, labels[i])
	}

	h.worker.Run(func(key TxTracingId, timestamp time.Time) func() {
		return func() {
			t, ok := h.traces[key]
			if !ok {
				h.errorHandler(fmt.Sprintf("error with tracer: %s: %s at end", h.name, key.String()))
			} else {
				t.span.SetAttributes(attributes...)
				t.span.End(trace.WithTimestamp(timestamp))
				delete(h.traces, key)
				h.histogram.WithLabelValues(labels...).Observe(float64(timestamp.Sub(t.start)))
			}
		}
	}(key, timestamp))

}

func (t *latencyTracer) Collectors() []prometheus.Collector {
	return []prometheus.Collector{t.histogram}
}

//TODO: AF Panic
func (h *latencyTracer) handleError(s string) {
	logger.Error(s)
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
