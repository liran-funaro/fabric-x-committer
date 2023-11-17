package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func NewNoOpProvider() Provider {
	return &noOpProvider{LatencyConfig: &LatencyConfig{}, errChan: make(chan error)}
}

type noOpProvider struct {
	*LatencyConfig
	errChan chan error
}

func (p *noOpProvider) StartPrometheusServer() <-chan error {
	return p.errChan
}
func (p *noOpProvider) StopServer() error {
	p.errChan <- nil
	return nil
}
func (p *noOpProvider) URL() string                                          { return "" }
func (p *noOpProvider) NewCounter(prometheus.CounterOpts) prometheus.Counter { return &noOpCounter{} }
func (p *noOpProvider) NewIntCounter(prometheus.CounterOpts) *IntCounter {
	return &IntCounter{Counter: &noOpCounter{}}
}
func (p *noOpProvider) NewIntGauge(prometheus.GaugeOpts) *IntGauge {
	return &IntGauge{Gauge: &noOpGauge{}}
}
func (p *noOpProvider) NewCounterVec(prometheus.CounterOpts, []string) *prometheus.CounterVec {
	return &prometheus.CounterVec{MetricVec: prometheus.NewMetricVec(&prometheus.Desc{}, func(...string) prometheus.Metric {
		return &noOpMetric{}
	})}
}
func (p *noOpProvider) NewGauge(prometheus.GaugeOpts) prometheus.Gauge { return &noOpGauge{} }
func (p *noOpProvider) NewGaugeVec(prometheus.GaugeOpts, []string) *prometheus.GaugeVec {
	return &prometheus.GaugeVec{MetricVec: prometheus.NewMetricVec(&prometheus.Desc{}, func(...string) prometheus.Metric {
		return &noOpGauge{}
	})}
}
func (p *noOpProvider) NewHistogram(prometheus.HistogramOpts) prometheus.Histogram {
	return &noOpHistogram{}
}
func (p *noOpProvider) NewHistogramVec(prometheus.HistogramOpts, []string) *prometheus.HistogramVec {
	return &prometheus.HistogramVec{MetricVec: prometheus.NewMetricVec(&prometheus.Desc{}, func(...string) prometheus.Metric {
		return &noOpHistogram{}
	})}
}

func (p *noOpProvider) Buckets() []float64 {
	return []float64{}
}

type noOpMetric struct{}

func (c *noOpMetric) Desc() *prometheus.Desc  { return nil }
func (c *noOpMetric) Write(*dto.Metric) error { return nil }

type noOpCollector struct{}

func (c *noOpCollector) Describe(chan<- *prometheus.Desc) {}
func (c *noOpCollector) Collect(chan<- prometheus.Metric) {}

type noOpCounter struct {
	*noOpMetric
	*noOpCollector
}

func (c *noOpCounter) Inc()        {}
func (c *noOpCounter) Add(float64) {}

type noOpGauge struct {
	*noOpMetric
	*noOpCollector
	*noOpCounter
}

func (c *noOpGauge) Set(float64)       {}
func (c *noOpGauge) Dec()              {}
func (c *noOpGauge) Sub(float64)       {}
func (c *noOpGauge) SetToCurrentTime() {}

type noOpHistogram struct {
	*noOpMetric
	*noOpCollector
}

func (c *noOpHistogram) Observe(float64) {}
