package main

import (
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
)

var logger = logging.New("load-generator")

type blockGenClient interface {
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

func createClient(c *ClientConfig, logger CmdLogger) (blockGenClient, *perfMetrics, error) {
	metrics := newBlockgenServiceMetrics(metrics.CreateProvider(c.Monitoring.Metrics))
	tracker := NewClientTracker(logger, metrics, &c.Monitoring.Metrics.Latency.SamplerConfig)

	if c.CoordinatorClient != nil {
		return newCoordinatorClient(c.CoordinatorClient, tracker, logger), metrics, nil
	}
	if c.VCClient != nil {
		return newVCClient(c.VCClient, tracker, logger), metrics, nil
	}
	if c.SidecarClient != nil {
		return newSidecarClient(c.SidecarClient, tracker, logger), metrics, nil
	}
	if c.SigVerifierClient != nil {
		return newSVClient(c.SigVerifierClient, tracker, logger), metrics, nil
	}
	return nil, nil, errors.New("invalid config passed")
}
