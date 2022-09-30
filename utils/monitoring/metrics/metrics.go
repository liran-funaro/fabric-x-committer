package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

var CpuUsage = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
	Name: "cpu_usage",
	Help: "Total CPU usage",
}, func() float64 {
	usage, _ := cpu.Percent(0, false)
	return usage[0]
})
var MemoryUsage = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
	Name: "memory_usage",
	Help: "Memory usage",
}, func() float64 {
	memory, _ := mem.VirtualMemory()
	return memory.UsedPercent
})

var AllMetrics = []prometheus.Collector{CpuUsage, MemoryUsage, CommitterChannelLength}

type ChannelBufferGauge struct {
	prometheus.Gauge
	capacity float64
}

var CommitterChannelLength = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "channel_length",
	Help: "The pending objects in a channel buffer",
}, []string{"sub_component", "channel"})

type BufferGaugeOpts struct {
	SubComponent, Channel string
}

func NewChannelBufferGauge(opts BufferGaugeOpts) ChannelBufferGauge {
	return ChannelBufferGauge{Gauge: CommitterChannelLength.With(prometheus.Labels{
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
