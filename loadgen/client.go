package loadgen

import (
	"log"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"google.golang.org/grpc"
)

type blockGenClient interface {
	Start(*BlockStreamGenerator) error
	Stop()
}

type loadGenClient struct {
	stopSender chan any
}

func (c *loadGenClient) Stop() {
	close(c.stopSender)
}

func createClient(c *ClientConfig) (blockGenClient, *PerfMetrics, error) { //nolint:ireturn
	logger.Infof("Config passed: %s", utils.LazyJson(c))
	m := newBlockgenServiceMetrics(metrics.CreateProvider(c.Monitoring.Metrics))

	if c.CoordinatorClient != nil {
		return newCoordinatorClient(c.CoordinatorClient, m), m, nil
	}
	if c.VCClient != nil {
		return newVCClient(c.VCClient, m), m, nil
	}
	if c.SidecarClient != nil {
		return newSidecarClient(c.SidecarClient, m), m, nil
	}
	if c.SigVerifierClient != nil {
		return newSVClient(c.SigVerifierClient, m), m, nil
	}
	return nil, nil, errors.New("invalid config passed")
}

func (c *loadGenClient) startSending(
	queue <-chan *protoblocktx.Block, stream grpc.ClientStream, send func(*protoblocktx.Block) error,
) error {
	defer func() {
		_ = stream.CloseSend()
	}()
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
				return connection.WrapStreamRpcError(err)
			}
		}
	}
}

// Starter starts the load generator with the given configuration.
func Starter( //nolint:revive,ireturn
	c *ClientConfig,
) (*PerfMetrics, *BlockStreamGenerator, blockGenClient, error) {
	client, metrics, err := createClient(c)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed creating client")
	}

	promErrChan := metrics.provider.StartPrometheusServer()

	go func() {
		if errProm := <-promErrChan; errProm != nil {
			log.Panic(err) // nolint: revive
		}
	}()

	blockGen := StartBlockGenerator(c.LoadProfile, c.RateLimit)

	return metrics, blockGen, client, nil
}
