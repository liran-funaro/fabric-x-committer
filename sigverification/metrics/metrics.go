package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

var TxsReceived = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "received_txs",
	Help: "The total number of processed TXs",
})
var TxsSent = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "sent_txs",
	Help: "The total number of sent TXs",
})
var BatchesReceived = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "received_batches",
	Help: "The total number of processed batches",
})
var BatchesSent = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "sent_batches",
	Help: "The total number of sent batches",
})
var ActiveStreams = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "active_streams",
	Help: "The total number of started streams",
})

var ParallelExecutorInputChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "parallel_executor",
	Channel:      "input",
})
var ParallelExecutorOutputChLength = metrics.NewChannelBufferGauge(metrics.BufferGaugeOpts{
	SubComponent: "parallel_executor",
	Channel:      "output",
})

var AllMetrics = []prometheus.Collector{TxsReceived, TxsSent, BatchesReceived, BatchesSent, ActiveStreams}
