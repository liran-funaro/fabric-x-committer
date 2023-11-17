package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type IntCounter struct{ prometheus.Counter }

func (c *IntCounter) Add(v int) { c.Counter.Add(float64(v)) }

type IntGauge struct{ prometheus.Gauge }

func (g *IntGauge) Set(v int) { g.Gauge.Set(float64(v)) }

type DurationHistogram struct{ prometheus.Histogram }

func (h *DurationHistogram) Observe(d time.Duration) { h.Histogram.Observe(float64(d)) }
