package workload

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"go.opentelemetry.io/otel/sdk/trace"
)

const ScrapingInterval = 15 * time.Second

type Metrics struct {
	generatorRequests  *prometheus.CounterVec
	generatorResponses *prometheus.CounterVec
	requestTracer      metrics.AppTracer
}

func NewMetrics() *Metrics {
	return &Metrics{
		generatorRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "e2e_requests",
			Help: "E2E requests sent by the generator",
		}, []string{"status"}),
		generatorResponses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "e2e_responses",
			Help: "E2E responses received by the generator",
		}, []string{"status"}),
		requestTracer: &metrics.NoopLatencyTracer{},
	}
}
func (m *Metrics) AllMetrics() []prometheus.Collector {
	return append(m.requestTracer.Collectors(), m.generatorRequests, m.generatorResponses)
}
func (m *Metrics) SetTracerProvider(tp *trace.TracerProvider) {
	m.requestTracer = metrics.NewLatencyTracer(metrics.LatencyTracerOpts{
		Name:           "generator_latency",
		Help:           "Total latency on the component",
		Count:          1000,
		From:           0,
		To:             20 * time.Second,
		Sampler:        metrics.NewPrefixSampler("1"),
		IgnoreNotFound: true,
		Labels:         []string{"status"},
	}, tp)
}

func (m *Metrics) IsEnabled() bool {
	return true
}

type MetricTracker struct {
	metrics *Metrics
}

func NewMetricTracker(p monitoring.Prometheus) *MetricTracker {
	m := NewMetrics()

	monitoring.LaunchPrometheus(p, monitoring.Generator, m)

	return &MetricTracker{m}
}

func (t *MetricTracker) RegisterEvent(e *Event) {
	switch e.Msg {
	case EventSubmitted:
		for i := uint64(0); i < uint64(e.SubmittedBlock.Size); i++ {
			t.RequestSent(token.TxSeqNum{e.SubmittedBlock.Id, i}, coordinatorservice.Status_UNKNOWN, e.Timestamp)
		}
	case EventReceived:
		for _, status := range e.StatusBatch.TxsValidationStatus {
			t.ResponseReceived(token.TxSeqNum{status.BlockNum, status.TxNum}, status.Status, e.Timestamp)
		}
	}
}

func (t *MetricTracker) RequestSent(txId metrics.TxTracingId, status coordinatorservice.Status, timestamp time.Time) {
	t.metrics.generatorRequests.WithLabelValues(status.String()).Add(1)
	t.metrics.requestTracer.StartAt(txId, timestamp)
}
func (t *MetricTracker) ResponseReceived(txId metrics.TxTracingId, status coordinatorservice.Status, timestamp time.Time) {
	t.metrics.requestTracer.EndAt(txId, timestamp, status.String())
	t.metrics.generatorResponses.WithLabelValues(status.String()).Add(1)
}
