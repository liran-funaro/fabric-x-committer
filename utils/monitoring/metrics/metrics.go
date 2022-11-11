package metrics

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Default Metrics

type AppMetrics interface {
	IsEnabled() bool
	AllMetrics() []prometheus.Collector
	SetTracerProvider(*trace.TracerProvider)
}

type Metrics struct {
	Enabled       bool
	GoRoutines    prometheus.GaugeFunc
	ComponentType prometheus.Gauge
}

func New(enabled bool) *Metrics {
	if !enabled {
		return &Metrics{Enabled: false}
	}
	return &Metrics{
		Enabled: true,
		GoRoutines: prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "go_routines",
			Help: "The total number of active GoRoutines",
		}, func() float64 {
			return float64(runtime.NumGoroutine())
		}),
		ComponentType: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "component_type",
			Help: "Current component type",
		}),
	}
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	if !m.Enabled {
		return []prometheus.Collector{}
	}
	return []prometheus.Collector{m.GoRoutines, m.ComponentType}
}

// Re-Usable Metrics

type ThroughputDirection = string

const (
	In  ThroughputDirection = "in"
	Out ThroughputDirection = "out"
)

type ThroughputCounter struct {
	*prometheus.CounterVec
}

var throughputCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "component_throughput",
	Help: "Incoming requests/Outgoing responses for a component",
}, []string{"sub_component", "direction"})

func NewThroughputCounter(subComponent string, direction ThroughputDirection) *ThroughputCounter {
	return &ThroughputCounter{CounterVec: throughputCounter.MustCurryWith(prometheus.Labels{"sub_component": subComponent, "direction": direction})}
}

func NewThroughputCounterVec(direction ThroughputDirection) *ThroughputCounter {
	return &ThroughputCounter{CounterVec: throughputCounter.MustCurryWith(prometheus.Labels{"direction": direction})}
}

func (c *ThroughputCounter) AddFor(subComponent string, length int) {
	c.CounterVec.WithLabelValues("sub_component", subComponent).Add(float64(length))
}

func (c *ThroughputCounter) Add(length int) {
	c.CounterVec.WithLabelValues().Add(float64(length))
}

type InMemoryDataStructureGauge struct {
	*prometheus.GaugeVec
}

func (g *InMemoryDataStructureGauge) Set(length int) {
	g.GaugeVec.WithLabelValues().Set(float64(length))
}

var inMemoryDataStructureGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "data_structure_size",
	Help: "Size of data structures (maps/slices)",
}, []string{"sub_component", "prop"})

func NewInMemoryDataStructureGauge(subComponent, prop string) *InMemoryDataStructureGauge {
	return &InMemoryDataStructureGauge{GaugeVec: inMemoryDataStructureGauge.MustCurryWith(prometheus.Labels{
		"sub_component": subComponent,
		"prop":          prop,
	}),
	}
}

type ChannelBufferGauge struct {
	*prometheus.GaugeVec
	capacity float64
}

var channelBufferLengthGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "channel_length",
	Help: "The pending objects in a channel buffer",
}, []string{"sub_component", "channel"})

type BufferGaugeOpts struct {
	SubComponent, Channel string
}

func NewChannelBufferGauge(opts BufferGaugeOpts) *ChannelBufferGauge {
	return &ChannelBufferGauge{GaugeVec: channelBufferLengthGauge.MustCurryWith(prometheus.Labels{
		"sub_component": opts.SubComponent,
		"channel":       opts.Channel,
	}),
	}
}

func (g *ChannelBufferGauge) SetCapacity(capacity int) {
	if capacity <= 0 {
		panic("only buffered channels supported")
	}
	g.capacity = float64(capacity)
}

func (g *ChannelBufferGauge) Set(length int) {
	if g.capacity == 0 {
		panic("no capacity set")
	}
	g.GaugeVec.WithLabelValues().Set(float64(length) / g.capacity)
}
