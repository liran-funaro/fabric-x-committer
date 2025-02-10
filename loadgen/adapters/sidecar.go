package adapters

import (
	"context"
	"math"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/broadcastdeliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/sidecarclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"golang.org/x/sync/errgroup"
)

type (
	// SidecarAdapter applies load on the sidecar.
	SidecarAdapter struct {
		commonAdapter
		config *SidecarClientConfig
	}
)

// NewSidecarAdapter instantiate SidecarAdapter.
func NewSidecarAdapter(config *SidecarClientConfig, res *ClientResources) *SidecarAdapter {
	return &SidecarAdapter{
		commonAdapter: commonAdapter{res: res},
		config:        config,
	}
}

// RunWorkload applies load on the sidecar.
func (c *SidecarAdapter) RunWorkload(ctx context.Context, txStream TxStream) error {
	coordinatorConn, err := connection.Connect(connection.NewDialConfig(c.config.Coordinator.Endpoint))
	if err != nil {
		return errors.Wrapf(
			err, "failed to connect to coordinator on %s", c.config.Coordinator.Endpoint.String(),
		)
	}
	defer connection.CloseConnectionsLog(coordinatorConn)
	coordinatorClient := protocoordinatorservice.NewCoordinatorClient(coordinatorConn)
	if err = c.setupCoordinator(ctx, coordinatorClient); err != nil {
		return err
	}

	ledgerReceiver, err := sidecarclient.New(&sidecarclient.Config{
		ChannelID: c.config.Orderer.ChannelID,
		Endpoint:  c.config.Endpoint,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create listener")
	}
	logger.Info("Listener created")

	broadcastSubmitter, err := broadcastdeliver.New(&c.config.Orderer)
	if err != nil {
		return errors.Wrap(err, "failed to create orderer clients")
	}
	defer broadcastSubmitter.Close()

	g, gCtx := errgroup.WithContext(ctx)

	committedBlock := make(chan *common.Block, 100)
	g.Go(func() error {
		return ledgerReceiver.Deliver(gCtx, &sidecarclient.DeliverConfig{
			EndBlkNum:   math.MaxUint64,
			OutputBlock: committedBlock,
		})
	})
	g.Go(func() error {
		c.receiveCommittedBlock(gCtx, committedBlock)
		return nil
	})

	for range c.config.BroadcastParallelism {
		g.Go(func() error {
			stream, err := broadcastSubmitter.Broadcast(gCtx)
			if err != nil {
				return err
			}
			g.Go(func() error {
				err := c.sendTransactions(gCtx, txStream, stream)
				// Insufficient quorum may happen when the context ends due to unavailable servers.
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			})
			return nil
		})
	}
	return g.Wait()
}

func (c *SidecarAdapter) sendTransactions(
	ctx context.Context, txStream TxStream, stream *broadcastdeliver.EnvelopedStream,
) error {
	txGen := txStream.MakeTxGenerator()
	for ctx.Err() == nil {
		tx := txGen.Next()
		if tx == nil {
			// If the context ended, the generator returns nil.
			break
		}
		txBytes, err := protoutil.Marshal(tx)
		if err != nil {
			return err
		}
		txID, resp, err := stream.SubmitWithEnv(txBytes)
		if err != nil {
			return err
		}
		logger.Debugf("Sent TX %s, got ack: %s", txID, resp.Info)
		c.res.Metrics.OnSendTransaction(txID)
	}
	return nil
}

func (c *SidecarAdapter) receiveCommittedBlock(ctx context.Context, blockQueue chan *common.Block) {
	committedBlock := channel.NewReader(ctx, blockQueue)
	for ctx.Err() == nil {
		block, ok := committedBlock.Read()
		if !ok {
			return
		}

		statusCodes := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
		logger.Infof("Received block [%d:%d] with %d status codes: %+v",
			block.Header.Number, len(block.Data.Data), len(statusCodes), statusCodes,
		)
		for i, data := range block.Data.Data {
			_, channelHeader, err := serialization.UnwrapEnvelope(data)
			if err != nil {
				logger.Warnf("Failed to unmarshal envelope: %v", err)
				continue
			}
			if common.HeaderType(channelHeader.Type) == common.HeaderType_CONFIG {
				// We can ignore config blocks as we only count data transactions.
				continue
			}
			c.res.Metrics.OnReceiveTransaction(
				channelHeader.TxId, protoblocktx.Status(statusCodes[i]),
			)
		}
	}
}
