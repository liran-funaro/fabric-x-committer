package latency

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
)

type AppTracer interface {
	Start(TxTracingId)
	StartAt(TxTracingId, time.Time)
	AddEvent(TxTracingId, string)
	AddEventAt(TxTracingId, string, time.Time)
	End(TxTracingId, ...attribute.KeyValue)
	EndAt(TxTracingId, time.Time, ...attribute.KeyValue)
	Collectors() []prometheus.Collector
}

type TxTracingSampler = func(key TxTracingId) bool

type TxTracingId = interface {
	String() string
}
