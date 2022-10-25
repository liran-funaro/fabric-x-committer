package workload

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

var (
	ScrapingInterval = 15 * time.Second

	generatorRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "e2e_requests",
		Help: "E2E requests sent by the generator",
	})
	generatorResponses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "e2e_responses",
		Help: "E2E responses received by the generator",
	}, []string{"status"})
)

type MetricTracker struct {
	latency *metrics.LatencyHistogram
}

func NewMetricTracker(p monitoring.Prometheus) *MetricTracker {
	histogram := metrics.NewDefaultLatencyHistogram("generator_latency", 5*time.Second, metrics.SampleThousandPerMillionUsing(metrics.Identity), "status")

	monitoring.LaunchPrometheus(p, monitoring.Generator, []prometheus.Collector{histogram, generatorRequests, generatorResponses})

	return &MetricTracker{latency: histogram}
}

func (t *MetricTracker) RegisterEvent(e *Event) {
	switch e.Msg {
	case EventSubmitted:
		t.latency.Begin(e.SubmittedBlock.Id, e.SubmittedBlock.Size, e.Timestamp)
		generatorRequests.Add(float64(e.SubmittedBlock.Size))
	case EventReceived:
		for _, status := range e.StatusBatch.TxsValidationStatus {
			t.latency.End(status.BlockNum, e.Timestamp, status.Status.String())
			generatorResponses.WithLabelValues(status.Status.String()).Add(1)
		}
	}
}
