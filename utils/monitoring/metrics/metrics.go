package metrics

import "github.com/prometheus/client_golang/prometheus"

type ThroughputDirection = string

const (
	In  ThroughputDirection = "in"
	Out ThroughputDirection = "out"
)

var throughputCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "component_throughput",
	Help: "Incoming requests/Outgoing responses for a component",
}, []string{"sub_component", "direction"})

func NewThroughputCounter(subComponent string, direction ThroughputDirection) prometheus.Counter {
	return throughputCounter.With(prometheus.Labels{"sub_component": subComponent, "direction": direction})
}

func NewThroughputCounterVec(direction ThroughputDirection) *prometheus.CounterVec {
	return throughputCounter.MustCurryWith(prometheus.Labels{"direction": direction})
}

type ChannelBufferGauge struct {
	prometheus.Gauge
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
	return &ChannelBufferGauge{Gauge: channelBufferLengthGauge.With(prometheus.Labels{
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
	g.Gauge.Set(float64(length) / g.capacity)
}
