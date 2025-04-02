package monitoring

import (
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/promutil"
)

type (
	// IntCounter modifies [prometheus.Counter] to count desecrate values.
	IntCounter struct{ prometheus.Counter }

	// IntGauge modifies [prometheus.Gauge] to monitor desecrate values.
	IntGauge struct{ prometheus.Gauge }

	// DurationHistogram modifies [prometheus.Histogram] to monitor [time.Duration].
	DurationHistogram struct{ prometheus.Histogram }

	// ThroughputDirection can in or out.
	ThroughputDirection = string

	// BufferGaugeOpts describes buffer gauge parameters.
	BufferGaugeOpts struct {
		Namespace string
		Subsystem string
		Channel   string
	}

	// ConnectionMetricsOpts describes connection metrics parameters.
	ConnectionMetricsOpts struct {
		Namespace       string
		RemoteNamespace string
	}

	// ConnectionMetrics supports common connection metrics.
	ConnectionMetrics struct {
		Status       *prometheus.GaugeVec
		FailureTotal *prometheus.CounterVec
		connected    atomic.Bool
	}
)

// Direction constants.
const (
	In  ThroughputDirection = "in"
	Out ThroughputDirection = "out"
)

// Add a value.
func (c *IntCounter) Add(v int) { c.Counter.Add(float64(v)) }

// Set a value.
func (g *IntGauge) Set(v int) { g.Gauge.Set(float64(v)) }

// Observe a duration.
func (h *DurationHistogram) Observe(d time.Duration) { h.Histogram.Observe(d.Seconds()) }

// Connected observed connected.
func (m *ConnectionMetrics) Connected(grpcTarget string) {
	promutil.SetGaugeVec(m.Status, []string{grpcTarget}, connection.Connected)
	m.connected.Store(true)
}

// Disconnected observe disconnected. The failure count is increased only if the status was connected.
func (m *ConnectionMetrics) Disconnected(grpcTarget string) {
	promutil.SetGaugeVec(m.Status, []string{grpcTarget}, connection.Disconnected)
	if m.connected.CompareAndSwap(true, false) {
		promutil.AddToCounterVec(m.FailureTotal, []string{grpcTarget}, 1)
	}
}
