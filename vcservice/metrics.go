package vcservice

import (
	"math"

	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
)

var timeBuckets = []float64{
	math.Inf(-1), 0,
	1e-9, 1e-8, 1e-7, 1e-6, 1e-5,
	1e-4, 2.5e-4, 5e-4, 7.5e-4,
	1e-3, 2.5e-3, 5e-3, 7.5e-3,
	1e-2, 2.5e-2, 5e-2, 7.5e-2,
	1e-1, 2.5e-1, 5e-1, 7.5e-1,
	1, 2.5, 5, 7.5,
	10, 25, 50, 75,
	1e2, 1e3, 1e4, 1e5, 1e6,
	math.Inf(1),
}

type perfMetrics struct {
	enabled                  bool
	provider                 *prometheusmetrics.Provider
	transactionReceivedTotal prometheus.Counter
	queueSize                *prometheus.GaugeVec
	processLatency           *prometheus.HistogramVec
}

func newVCServiceMetrics(enabled bool) *perfMetrics {
	p := prometheusmetrics.NewProvider()

	return &perfMetrics{
		enabled:  enabled,
		provider: p,
		transactionReceivedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "vcservice",
			Subsystem: "grpc",
			Name:      "received_transaction_total",
			Help:      "Number of transactions received by the service",
		}),
		queueSize: p.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "vcservice",
			Name:      "queue_size_txs",
			Help:      "The service queue sizes",
		}, []string{"queue_type"}),
		processLatency: p.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "vcservice",
			Name:      "latency_seconds",
			Buckets:   timeBuckets,
			Help:      "The service processes latency",
		}, []string{"process"}),
	}
}

func (s *perfMetrics) grpcReceive(size int) {
	if s.enabled {
		s.transactionReceivedTotal.Add(float64(size))
	}
}

func (s *perfMetrics) queue(label string, size int) {
	if s.enabled {
		s.queueSize.WithLabelValues(label).Set(float64(size))
	}
}

type timer prometheus.Timer

func (s *perfMetrics) newLatencyTimer(process string) *timer {
	if !s.enabled {
		return nil
	}
	return (*timer)(prometheus.NewTimer(s.processLatency.WithLabelValues(process)))
}

func (t *timer) observe() {
	if t == nil {
		return
	}
	(*prometheus.Timer)(t).ObserveDuration()
}
