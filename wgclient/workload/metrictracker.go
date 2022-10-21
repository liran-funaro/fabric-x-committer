package workload

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

var ScrapingInterval = 15 * time.Second

type MetricTracker struct {
	latency *metrics.LatencyHistogram
}

func NewMetricTracker(p monitoring.Prometheus) *MetricTracker {
	histogram := metrics.NewLatencyHistogram(metrics.LatencyHistogramOpts{
		Name:   "generator_latency",
		Help:   "Latency of responses that the generator receives from the coordinator",
		Count:  1000,
		From:   0,
		To:     10 * time.Second,
		Labels: []string{"status"},
	})

	monitoring.LaunchPrometheus(p, monitoring.Generator, []prometheus.Collector{histogram})

	return &MetricTracker{latency: histogram}
}

func (t *MetricTracker) RegisterEvent(e *Event) {
	switch e.Msg {
	case EventSubmitted:
		t.latency.Begin(e.SubmittedBlock.Id, e.SubmittedBlock.Size, e.Timestamp)
	case EventReceived:
		for _, status := range e.StatusBatch.TxsValidationStatus {
			t.latency.End(status.BlockNum, e.Timestamp, status.Status.String())
		}
	}
}
