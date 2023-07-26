package vcservice

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
)

var receivedTransactionTotalOpts = prometheus.CounterOpts{
	Namespace: "vcservice",
	Subsystem: "grpc",
	Name:      "received_transaction_total",
	Help:      "Number of transactions received by the service",
}

type perfMetrics struct {
	transactionReceivedTotal prometheus.Counter
}

func newVCServiceMetrics(p *prometheusmetrics.Provider) *perfMetrics {
	return &perfMetrics{
		transactionReceivedTotal: p.NewCounter(receivedTransactionTotalOpts),
	}
}
