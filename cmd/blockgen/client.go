package main

import (
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
)

var logger = logging.New("load-generator")

type blockGenClient interface {
	Start(*loadgen.BlockStreamGenerator) error
	Stop()
}

type loadGenClient struct {
	tracker    tracker.Sender
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

	if c.CoordinatorClient != nil {
		return newCoordinatorClient(c.CoordinatorClient, metrics, logger), metrics, nil
	}
	if c.VCClient != nil {
		return newVCClient(c.VCClient, metrics, logger), metrics, nil
	}
	if c.SidecarClient != nil {
		return newSidecarClient(c.SidecarClient, metrics, logger), metrics, nil
	}
	if c.SigVerifierClient != nil {
		return newSVClient(c.SigVerifierClient, metrics, logger), metrics, nil
	}
	return nil, nil, errors.New("invalid config passed")
}
