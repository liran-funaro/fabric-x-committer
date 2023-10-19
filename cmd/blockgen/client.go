package main

import (
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
)

type LoadGenClient interface {
	Start(*loadgen.BlockStreamGenerator) error
	Stop()
}

type loadGenClient struct {
	tracker    *ClientTracker
	logger     CmdLogger
	stopSender chan any
}

func (c *loadGenClient) Stop() {
	for i := 0; i < cap(c.stopSender); i++ {
		c.stopSender <- struct{}{}
	}
}

func createClient(c *ClientConfig, logger CmdLogger) (LoadGenClient, *perfMetrics, error) {
	metrics := newBlockgenServiceMetrics(c.Monitoring.Metrics.Enable)
	tracker := NewClientTracker(logger, metrics, c.Monitoring.Latency.Sampler)

	if c.CoordinatorClient != nil {
		return NewCoordinatorClient(c.CoordinatorClient, tracker, logger), metrics, nil
	}
	if c.VCClient != nil {
		return NewVCClient(c.VCClient, tracker, logger), metrics, nil
	}
	if c.SidecarClient != nil {
		return NewSidecarClient(c.SidecarClient, tracker, logger), metrics, nil
	}
	return nil, nil, errors.New("invalid config passed")
}
