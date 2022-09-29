package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

var CpuUsage = promauto.NewGaugeFunc(prometheus.GaugeOpts{
	Name: "cpu_usage",
	Help: "Total CPU usage",
}, func() float64 {
	usage, _ := cpu.Percent(0, false)
	return usage[0]
})
var MemoryUsage = promauto.NewGaugeFunc(prometheus.GaugeOpts{
	Name: "memory_usage",
	Help: "Memory usage",
}, func() float64 {
	memory, _ := mem.VirtualMemory()
	return memory.UsedPercent
})

type ChannelBufferGauge struct {
	gauge    prometheus.Gauge
	capacity float64
}

var committerChannelLength = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sc_channel_length",
	Help: "The pending objects in a channel buffer",
}, []string{"component", "sub_component", "channel"})

func NewChannelBufferGauge(component, subComponent, channel string, capacity int) *ChannelBufferGauge {
	if capacity <= 0 {
		panic("only buffered channels allowed")
	}
	return &ChannelBufferGauge{
		gauge: committerChannelLength.With(prometheus.Labels{
			"component":     component,
			"sub_component": subComponent,
			"channel":       channel,
		}),
		capacity: float64(capacity),
	}
}

func (g *ChannelBufferGauge) Set(length int) {
	g.gauge.Set(float64(length) / g.capacity)
}
