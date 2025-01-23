package adapters

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"golang.org/x/sync/errgroup"
)

type (
	// CoordinatorAdapter applies load on the coordinator.
	CoordinatorAdapter struct {
		commonAdapter
		config *CoordinatorClientConfig
	}
)

// NewCoordinatorAdapter instantiate CoordinatorAdapter.
func NewCoordinatorAdapter(config *CoordinatorClientConfig, res *ClientResources) *CoordinatorAdapter {
	return &CoordinatorAdapter{
		commonAdapter: commonAdapter{res: res},
		config:        config,
	}
}

// RunWorkload applies load on the coordinator.
func (c *CoordinatorAdapter) RunWorkload(ctx context.Context, txStream TxStream) error {
	conn, err := connection.Connect(connection.NewDialConfig(c.config.Endpoint))
	if err != nil {
		return errors.Wrapf(err, "failed to connect to %s", c.config.Endpoint.String())
	}
	defer connection.CloseConnectionsLog(conn)
	client := protocoordinatorservice.NewCoordinatorClient(conn)
	if err = c.setupCoordinator(ctx, client); err != nil {
		return err
	}
	if lastBlockNum, coordErr := client.GetLastCommittedBlockNumber(ctx, nil); coordErr != nil {
		// We do not return error as we can proceed assuming no blocks were committed.
		logger.Infof("cannot fetch the last committed block number: %v", coordErr)
	} else {
		c.nextBlockNum.Store(lastBlockNum.Number + 1)
	}

	logger.Info("Opening stream")
	stream, err := client.BlockProcessing(ctx)
	if err != nil {
		return errors.Wrap(err, "failed creating stream to coordinator")
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return c.sendBlocks(gCtx, txStream, stream.Send)
	})
	g.Go(func() error {
		return c.receiveStatus(gCtx, stream)
	})
	return g.Wait()
}

// Progress a submitted block indicates progress for the coordinator as it guaranteed to preserve the order.
func (c *CoordinatorAdapter) Progress() uint64 {
	return c.nextBlockNum.Load()
}

func (c *CoordinatorAdapter) receiveStatus(
	ctx context.Context, stream protocoordinatorservice.Coordinator_BlockProcessingClient,
) error {
	for ctx.Err() == nil {
		txStatus, err := stream.Recv()
		if err != nil {
			return connection.WrapStreamRpcError(err)
		}

		logger.Debugf("Received coordinator status batch with %d items", len(txStatus.TxsValidationStatus))
		for _, tx := range txStatus.TxsValidationStatus {
			c.res.Metrics.OnReceiveTransaction(tx.TxId, tx.Status)
		}
	}
	return nil
}
