package metrics

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
)

func CreateProvider(c *Config) Provider {
	if !c.Enable {
		return NewNoOpProvider()
	}
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
