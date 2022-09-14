package performance

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var TxsReceived = promauto.NewCounter(prometheus.CounterOpts{
	Name: "sc_received_txs",
	Help: "The total number of processed TXs",
})
var TxsSent = promauto.NewCounter(prometheus.CounterOpts{
	Name: "sc_sent_txs",
	Help: "The total number of sent TXs",
})
var BatchesReceived = promauto.NewCounter(prometheus.CounterOpts{
	Name: "sc_received_batches",
	Help: "The total number of processed batches",
})
var BatchesSent = promauto.NewCounter(prometheus.CounterOpts{
	Name: "sc_sent_batches",
	Help: "The total number of sent batches",
})
var ActiveStreams = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "sc_active_streams",
	Help: "The total number of started streams",
})
