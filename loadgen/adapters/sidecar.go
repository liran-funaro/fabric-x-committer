package adapters

import (
	"context"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters/broadcastclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"golang.org/x/sync/errgroup"
)

type (
	// SidecarAdapter applies load on the sidecar.
	SidecarAdapter struct {
		commonAdapter
		config          *SidecarClientConfig
		envelopeCreator broadcastclient.EnvelopeCreator
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
	coordinatorConn, err := connection.Connect(connection.NewDialConfig(*c.config.Coordinator.Endpoint))
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

	committedBlock := make(chan *common.Block, 100)
	ledgerReceiver, err := deliverclient.New(&deliverclient.Config{
		ChannelID: c.config.Orderer.ChannelID,
		Endpoint:  *c.config.Endpoint,
		Reconnect: -1,
	}, deliverclient.Ledger, committedBlock)
	if err != nil {
		return errors.Wrap(err, "failed to create listener")
	}
	logger.Info("Listener created")

	g, gCtx := errgroup.WithContext(ctx)

	broadcastSubmitter, err := broadcastclient.New(gCtx, c.config.Orderer)
	if err != nil {
		return errors.Wrap(err, "failed to create orderer clients")
	}
	defer broadcastSubmitter.Close()
	c.envelopeCreator = broadcastSubmitter.EnvelopeCreator

	deliverSubmitter, err := broadcastclient.New(gCtx, c.config.Orderer)
	if err != nil {
		return errors.Wrap(err, "failed to create orderer clients")
	}
	defer deliverSubmitter.Close()

	g.Go(func() error {
		return ledgerReceiver.Run(gCtx, &deliverclient.ReceiverRunConfig{})
	})
	for _, stream := range broadcastSubmitter.Streams {
		g.Go(func() error {
			return c.sendTransactions(gCtx, txStream, stream)
		})
	}
	for _, stream := range deliverSubmitter.Streams {
		g.Go(func() error {
			return receiveResponses(gCtx, stream)
		})
	}
	g.Go(func() error {
		return c.receiveCommittedBlock(gCtx, committedBlock)
	})
	return g.Wait()
}

func (c *SidecarAdapter) sendTransactions(
	ctx context.Context, txStream TxStream, stream ab.AtomicBroadcast_BroadcastClient,
) error {
	txGen := txStream.MakeTxGenerator()
	for ctx.Err() == nil {
		tx := txGen.Next()
		if tx == nil {
			// If the context ended, the generator returns nil.
			break
		}
		env, txID, err := c.envelopeCreator.CreateEnvelope(protoutil.MarshalOrPanic(tx))
		if err != nil {
			return errors.Wrapf(err, "failed enveloping tx %s", txID)
		}
		logger.Debugf("Sending TX %s", txID)
		if err := stream.Send(env); err != nil {
			return connection.WrapStreamRpcError(err)
		}
		c.res.Metrics.OnSendTransaction(txID)
	}
	return nil
}

func receiveResponses(ctx context.Context, stream ab.AtomicBroadcast_BroadcastClient) error {
	for ctx.Err() == nil {
		response, err := stream.Recv()
		switch {
		case err != nil:
			return connection.WrapStreamRpcError(err)
		case response.Status != common.Status_SUCCESS:
			return errors.Errorf("unexpected status: %v - %s", response.Status, response.Info)
		default:
			logger.Debugf("Received ack for TX")
		}
	}
	return nil
}

func (c *SidecarAdapter) receiveCommittedBlock(ctx context.Context, blockQueue chan *common.Block) error {
	committedBlock := channel.NewReader(ctx, blockQueue)
	for ctx.Err() == nil {
		block, ok := committedBlock.Read()
		if !ok {
			return nil
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
	return nil
}
