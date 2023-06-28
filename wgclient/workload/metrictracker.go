package workload

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"go.opentelemetry.io/otel/attribute"
)

const ScrapingInterval = 15 * time.Second
const StatusLabel = "status"

type Metrics struct {
	generatorRequests  prometheus.Counter
	generatorResponses *prometheus.CounterVec
	requestTracer      latency.AppTracer
}

type generatorMonitoringProvider struct {
}

func (p *generatorMonitoringProvider) ComponentName() string {
	return "generator"
}
func (p *generatorMonitoringProvider) LatencyLabels() []string {
	return []string{StatusLabel}
}
func (p *generatorMonitoringProvider) NewMonitoring(enabled bool, tracer latency.AppTracer) metrics.AppMetrics {
	return &Metrics{
		generatorRequests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "e2e_requests",
			Help: "E2E requests sent by the generator",
		}),
		generatorResponses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "e2e_responses",
			Help: "E2E responses received by the generator",
		}, []string{StatusLabel}),
		requestTracer: tracer,
	}
}
func (m *Metrics) AllMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		m.generatorRequests,
		m.generatorResponses,
	}
}

type MetricTracker struct {
	metrics *Metrics
}

func NewMetricTracker(p monitoring.Config) *MetricTracker {
	m := monitoring.LaunchMonitoring(p, &generatorMonitoringProvider{}).(*Metrics)

	return &MetricTracker{m}
}

func (t *MetricTracker) TxSentAt(txId latency.TxTracingId, timestamp time.Time) {
	t.metrics.generatorRequests.Add(1)
	t.metrics.requestTracer.StartAt(txId, timestamp)
}
func (t *MetricTracker) TxReceivedAt(txId latency.TxTracingId, status coordinatorservice.Status, timestamp time.Time) {
	t.metrics.requestTracer.EndAt(txId, timestamp, attribute.String(StatusLabel, status.String()))
	t.metrics.generatorResponses.WithLabelValues(status.String()).Add(1)
}
