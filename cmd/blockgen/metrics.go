package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
)

var buckets = []float64{
	.0001, .001, .002, .003, .004, .005, .01, .03, .05,
	.1, .3, .5, 1, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 2.0,
	2.1, 2.2, 2.3, 2.4, 2.5, 3, 4, 5,
}

type perfMetrics struct {
	enabled                         bool
	provider                        *prometheusmetrics.Provider
	blockSentTotal                  prometheus.Counter
	transactionSentTotal            prometheus.Counter
	transactionReceivedTotal        prometheus.Counter
	validTransactionLatencySecond   prometheus.Histogram
	invalidTransactionLatencySecond prometheus.Histogram
}

func newBlockgenServiceMetrics(enabled bool) *perfMetrics {
	p := prometheusmetrics.NewProvider()

	return &perfMetrics{
		enabled:  enabled,
		provider: p,
		blockSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "block_sent_total",
			Help:      "Total number of blocks sent by the block generator",
		}),
		transactionSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_sent_total",
			Help:      "Total number of transactions sent by the block generator",
		}),
		transactionReceivedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_received_total",
			Help:      "Total number of transactions received by the block generator",
		}),
		validTransactionLatencySecond: p.NewHistogram(prometheus.HistogramOpts{
			Namespace: "blockgen",
			Subsystem: "",
			Name:      "valid_transaction_latency_seconds",
			Help:      "Latency of transactions in seconds",
			Buckets:   buckets,
		}),
		invalidTransactionLatencySecond: p.NewHistogram(prometheus.HistogramOpts{
			Namespace: "blockgen",
			Subsystem: "",
			Name:      "invalid_transaction_latency_seconds",
			Help:      "Latency of invalid transactions in seconds",
			Buckets:   buckets,
		}),
	}
}

func (s *perfMetrics) addToCounter(c prometheus.Counter, n int) {
	if s.enabled {
		c.Add(float64(n))
	}
}
