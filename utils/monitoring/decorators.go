package monitoring

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
