package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
)

func CreateProvider(c *Config) Provider {
	if !c.Enable {
		logger.Infof("NoOp metrics provider created")
		return NewNoOpProvider()
	}
	logger.Infof("Default metrics provider created")
	return &defaultProvider{
		LatencyConfig:  &c.Latency,
		Provider:       prometheusmetrics.NewProvider(),
		serverEndpoint: c.Endpoint,
	}
}

type defaultProvider struct {
	*LatencyConfig
	*prometheusmetrics.Provider
	serverEndpoint *connection.Endpoint
}

func (p *defaultProvider) StartPrometheusServer() <-chan error {
	return p.Provider.StartPrometheusServer(p.serverEndpoint)
}

func (p *defaultProvider) NewIntCounter(opts prometheus.CounterOpts) *IntCounter {
	return &IntCounter{Counter: p.NewCounter(opts)}
}

func (p *defaultProvider) NewIntGauge(opts prometheus.GaugeOpts) *IntGauge {
	return &IntGauge{Gauge: p.NewGauge(opts)}
}
