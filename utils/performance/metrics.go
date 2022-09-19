package performance

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
