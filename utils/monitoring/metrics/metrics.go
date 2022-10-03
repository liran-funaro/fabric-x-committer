package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/net"
)

type Metrics struct {
	samplingRate time.Duration
	Disk         *prometheus.CounterVec
	Network      *prometheus.CounterVec
}

func New() *Metrics {
	m := &Metrics{
		samplingRate: 1 * time.Second,
		Disk: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "disk_io",
			Help: "Disk I/O Utilization",
		}, []string{"name", "direction"}),
		Network: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "net_io",
			Help: "Network Utilization",
		}, []string{"name", "direction"}),
	}
	go m.track()
	return m
}

func (m *Metrics) AllMetrics() []prometheus.Collector {
	return []prometheus.Collector{m.Disk, m.Network}
}

func (m *Metrics) track() {
	for {
		<-time.After(m.samplingRate)
		if counters, err := net.IOCounters(true); err == nil {
			for _, counter := range counters {
				m.Network.With(prometheus.Labels{"name": counter.Name, "direction": In}).Add(float64(counter.BytesRecv))
				m.Network.With(prometheus.Labels{"name": counter.Name, "direction": Out}).Add(float64(counter.BytesSent))
			}
		}
		if counters, err := disk.IOCounters("sda"); err == nil {
			for _, counter := range counters {
				m.Disk.With(prometheus.Labels{"name": counter.Name, "direction": In}).Add(float64(counter.ReadBytes))
				m.Disk.With(prometheus.Labels{"name": counter.Name, "direction": Out}).Add(float64(counter.WriteBytes))
			}
		}
	}
}

func (m *Metrics) add(name string, bytesRecv, bytesSent uint64) {
	m.Network.With(prometheus.Labels{"name": name, "direction": In}).Add(float64(bytesRecv))
	m.Network.With(prometheus.Labels{"name": name, "direction": Out}).Add(float64(bytesSent))
}

type ThroughputDirection = string

const (
	In  ThroughputDirection = "in"
	Out ThroughputDirection = "out"
)

var ThroughputCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "component_throughput",
	Help: "Incoming requests/Outgoing responses for a component",
}, []string{"sub_component", "direction"})

func NewThroughputCounter(subComponent string, direction ThroughputDirection) prometheus.Counter {
	return ThroughputCounter.With(prometheus.Labels{"sub_component": subComponent, "direction": direction})
}

func NewThroughputCounterVec(direction ThroughputDirection) *prometheus.CounterVec {
	return ThroughputCounter.MustCurryWith(prometheus.Labels{"direction": direction})
}

type ChannelBufferGauge struct {
	prometheus.Gauge
	capacity float64
}

var ChannelBufferLengthGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "channel_length",
	Help: "The pending objects in a channel buffer",
}, []string{"sub_component", "channel"})

type BufferGaugeOpts struct {
	SubComponent, Channel string
}

func NewChannelBufferGauge(opts BufferGaugeOpts) *ChannelBufferGauge {
	return &ChannelBufferGauge{Gauge: ChannelBufferLengthGauge.With(prometheus.Labels{
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
