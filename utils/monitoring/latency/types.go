package latency

import (
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

type AppTracer interface {
	Start(TxTracingId)
	StartAt(TxTracingId, time.Time)
	AddEvent(TxTracingId, string)
	AddEventAt(TxTracingId, string, time.Time)
	End(TxTracingId, ...string)
	EndAt(TxTracingId, time.Time, ...string)
	Collectors() []prometheus.Collector
}

type TxTracingSampler = func(key TxTracingId) bool

type TxTracingId = interface {
	String() string
}
