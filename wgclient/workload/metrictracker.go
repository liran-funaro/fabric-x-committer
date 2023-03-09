package workload

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

const ScrapingInterval = 15 * time.Second

type Metrics struct {
	generatorRequests  *prometheus.CounterVec
	generatorResponses *prometheus.CounterVec
	requestTracer      latency.AppTracer
}

type Provider struct {
}

func (p *Provider) ComponentName() string {
	return "generator"
}
func (p *Provider) LatencyLabels() []string {
	return []string{"status"}
}
func (p *Provider) NewMonitoring(enabled bool, tracer latency.AppTracer) metrics.AppMetrics {
	return &Metrics{
		generatorRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "e2e_requests",
			Help: "E2E requests sent by the generator",
		}, []string{"status"}),
		generatorResponses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "e2e_responses",
			Help: "E2E responses received by the generator",
		}, []string{"status"}),
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
	m := monitoring.LaunchMonitoring(p, monitoring.Generator, &Provider{}).(*Metrics)

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

func (t *MetricTracker) RequestSent(txId latency.TxTracingId, status coordinatorservice.Status, timestamp time.Time) {
	t.metrics.generatorRequests.WithLabelValues(status.String()).Add(1)
	t.metrics.requestTracer.StartAt(txId, timestamp)
}
func (t *MetricTracker) ResponseReceived(txId latency.TxTracingId, status coordinatorservice.Status, timestamp time.Time) {
	t.metrics.requestTracer.EndAt(txId, timestamp, status.String())
	t.metrics.generatorResponses.WithLabelValues(status.String()).Add(1)
}
