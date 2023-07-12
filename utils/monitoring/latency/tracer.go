package latency

import (
	"context"
	"fmt"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/workerpool"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var logger = logging.New("latencytracer")

type traceData struct {
	start time.Time
	span  trace.Span
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
}

type TracerOpts struct {
	Name           string
	Help           string
	Count          int
	From, To       time.Duration
	Sampler        TxTracingSampler
	IgnoreNotFound bool
	Labels         []string
}

type NoOpTracer struct{}

func (t *NoOpTracer) Start(TxTracingId)                                   {}
func (t *NoOpTracer) StartAt(TxTracingId, time.Time)                      {}
func (t *NoOpTracer) AddEvent(TxTracingId, string)                        {}
func (t *NoOpTracer) AddEventAt(TxTracingId, string, time.Time)           {}
func (t *NoOpTracer) End(TxTracingId, ...attribute.KeyValue)              {}
func (t *NoOpTracer) EndAt(TxTracingId, time.Time, ...attribute.KeyValue) {}
func (t *NoOpTracer) Collectors() []prometheus.Collector {
	return []prometheus.Collector{}
}

func NewLatencyTracer(opts TracerOpts, tp *sdktrace.TracerProvider) *latencyTracer {
	return &latencyTracer{
		enabled: true,
		histogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    opts.Name,
			Help:    opts.Help,
			Buckets: utils.UniformBuckets(opts.Count, float64(opts.From), float64(opts.To)),
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

func (h *latencyTracer) End(key TxTracingId, attributes ...attribute.KeyValue) {
	h.EndAt(key, time.Now(), attributes...)
}

func (h *latencyTracer) EndAt(key TxTracingId, timestamp time.Time, attributes ...attribute.KeyValue) {
	if !h.enabled || !h.sampler(key) {
		return
	}

	labels := make(prometheus.Labels, len(attributes))
	for _, attr := range attributes {
		labels[string(attr.Key)] = attr.Value.AsString()
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
				h.histogram.With(labels).Observe(float64(timestamp.Sub(t.start)))
			}
		}
	}(key, timestamp))

}

func (t *latencyTracer) Collectors() []prometheus.Collector {
	return []prometheus.Collector{t.histogram}
}
