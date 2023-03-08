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
	m.requestTracer = metrics.NewDefaultLatencyTracer("generator_latency", 5*time.Second, tp, "status")
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
			t.metrics.requestTracer.StartAt(token.TxSeqNum{e.SubmittedBlock.Id, i}, e.Timestamp)
		}
		t.RequestSent(e.SubmittedBlock.Size, coordinatorservice.Status_UNKNOWN)
	case EventReceived:
		for _, status := range e.StatusBatch.TxsValidationStatus {
			t.metrics.requestTracer.EndAt(token.TxSeqNum{status.BlockNum, status.TxNum}, e.Timestamp, status.Status.String())
			t.ResponseReceived(1, status.Status)
		}
	}
}

func (t *MetricTracker) RequestSent(size int, status coordinatorservice.Status) {
	t.metrics.generatorRequests.WithLabelValues(status.String()).Add(float64(size))
}
func (t *MetricTracker) ResponseReceived(size int, status coordinatorservice.Status) {
	t.metrics.generatorResponses.WithLabelValues(status.String()).Add(float64(size))
}
