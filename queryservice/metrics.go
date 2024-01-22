package queryservice

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
)

var buckets = []float64{.0001, .001, .002, .003, .004, .005, .01, .03, .05, .1, .3, .5, 1}

type perfMetrics struct {
	*prometheusmetrics.Provider

	queriesReceivedTotal prometheus.Counter
	keyQueriedTotal      prometheus.Counter
	queryLatencySeconds  prometheus.Histogram
}

func newQueryServiceMetrics() *perfMetrics {
	p := prometheusmetrics.NewProvider()

	return &perfMetrics{
		Provider: p,
		queriesReceivedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "queryservice",
			Subsystem: "grpc",
			Name:      "received_query_total",
			Help:      "Number of queries received by the service",
		}),
		keyQueriedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "queryservice",
			Subsystem: "grpc",
			Name:      "queried_keys_total",
			Help:      "Number of keys queried by the service",
		}),
		queryLatencySeconds: p.NewHistogram(prometheus.HistogramOpts{
			Namespace: "queryservice",
			Subsystem: "database",
			Name:      "query_latency_seconds",
			Help:      "The latency of the query",
			Buckets:   buckets,
		}),
	}
}
