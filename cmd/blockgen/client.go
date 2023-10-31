package main

import (
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"google.golang.org/grpc"
)

var logger = logging.New("load-generator")

type blockGenClient interface {
	Start(*loadgen.BlockStreamGenerator) error
	Stop()
}

type loadGenClient struct {
	tracker    tracker.Sender
	stopSender chan any
}

func (c *loadGenClient) Stop() {
	close(c.stopSender)
}

func createClient(c *ClientConfig) (blockGenClient, *perfMetrics, error) {
	logger.Infof("Config passed: %s", utils.LazyJson(c))
	metrics := newBlockgenServiceMetrics(metrics.CreateProvider(c.Monitoring.Metrics))

	if c.CoordinatorClient != nil {
		return newCoordinatorClient(c.CoordinatorClient, metrics), metrics, nil
	}
	if c.VCClient != nil {
		return newVCClient(c.VCClient, metrics), metrics, nil
	}
	if c.SidecarClient != nil {
		return newSidecarClient(c.SidecarClient, metrics), metrics, nil
	}
	if c.SigVerifierClient != nil {
		return newSVClient(c.SigVerifierClient, metrics), metrics, nil
	}
	return nil, nil, errors.New("invalid config passed")
}

func (c *loadGenClient) startSending(queue <-chan *protoblocktx.Block, stream grpc.ClientStream, send func(*protoblocktx.Block) error) error {
	defer stream.CloseSend()
	for {
		select {
		case <-c.stopSender:
			logger.Infof("stopping senders")
			return nil
		case block, ok := <-queue:
			if !ok {
				logger.Infof("block generator terminated")
				return nil
			}
			if err := send(block); err != nil {
				return errors.Wrap(err, "failed sending")
			}
			c.tracker.OnSendBlock(block)
		}
	}
}
