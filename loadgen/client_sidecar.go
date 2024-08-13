package loadgen

import (
	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/broadcastclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
)

type sidecarClient struct {
	*loadGenClient
	coordinator     protocoordinatorservice.CoordinatorClient
	envelopeCreator broadcastclient.EnvelopeCreator
	sidecar         deliverclient.Client
	broadcasts      []ab.AtomicBroadcast_BroadcastClient
	delivers        []ab.AtomicBroadcast_BroadcastClient
	tracker         *sidecarTracker
}

func newSidecarClient(config *SidecarClientConfig, metrics *PerfMetrics) *sidecarClient {
	conn, err := connection.Connect(connection.NewDialConfig(*config.Coordinator.Endpoint))
	if err != nil {
		panic(errors.Wrapf(err, "failed to connect to coordinator on %s", config.Coordinator.Endpoint.String()))
	}
	coordinatorClient := openCoordinatorClient(conn)

	listener, err := deliverclient.New(&deliverclient.Config{
		ChannelID:                config.Orderer.ChannelID,
		Endpoint:                 *config.Endpoint,
		OrdererConnectionProfile: nil,
		Reconnect:                -1,
	}, &deliverclient.PeerDeliverClientProvider{})
	if err != nil {
		panic(errors.Wrap(err, "failed to create listener"))
	}
	logger.Info("Listener created")

	broadcastClients, envelopeCreator, err := broadcastclient.New(config.Orderer)
	if err != nil {
		panic(errors.Wrap(err, "failed to create orderer clients"))
	}

	delivers, _, err := broadcastclient.New(config.Orderer)
	if err != nil {
		panic(errors.Wrap(err, "failed to create orderer clients"))
	}

	return &sidecarClient{
		loadGenClient:   newLoadGenClient(),
		tracker:         newSidecarTracker(metrics),
		coordinator:     coordinatorClient,
		sidecar:         listener,
		broadcasts:      broadcastClients,
		delivers:        delivers,
		envelopeCreator: envelopeCreator,
	}
}

func (c *sidecarClient) Stop() {
	logger.Infof("Stopping sidecar client")
	c.sidecar.Stop()
	c.loadGenClient.Stop()
}

func (c *sidecarClient) Start(blockGen *BlockStream) error {
	if _, err := c.coordinator.SetMetaNamespaceVerificationKey(
		c.ctx,
		&protosigverifierservice.Key{SerializedBytes: blockGen.Signer.GetVerificationKey()},
	); err != nil {
		return errors.Wrap(err, "failed connecting to coordinator")
	}
	logger.Infof("Set verification key")

	go func() {
		if err := c.sidecar.RunDeliverOutputListener(c.ctx, c.tracker.OnReceiveSidecarBlock); err != nil {
			c.cancel(err)
		}
	}()

	for _, stream := range c.broadcasts {
		go c.startSending(blockGen, stream)
	}

	for _, stream := range c.delivers {
		go c.startReceiving(stream)
	}

	return nil
}

func (c *sidecarClient) startReceiving(stream ab.AtomicBroadcast_BroadcastClient) {
	for c.ctx.Err() == nil {
		if response, err := stream.Recv(); err != nil {
			c.cancel(errors.Wrapf(err, "failed receiving"))
		} else if response.Status != common.Status_SUCCESS {
			c.cancel(errors.Errorf("unexpected status: %v - %s", response.Status, response.Info))
		} else {
			logger.Debugf("Received ack for TX")
		}
	}
}

func (c *sidecarClient) startSending(
	blockGen *BlockStream, stream ab.AtomicBroadcast_BroadcastClient,
) {
	c.loadGenClient.startSending(blockGen, stream, func(block *protoblocktx.Block) error {
		logger.Debugf("Sending block %d with %d TXs", block.Number, len(block.Txs))
		for _, tx := range block.Txs {
			env, txID, err := c.envelopeCreator.CreateEnvelope(protoutil.MarshalOrPanic(tx))
			if err != nil {
				return errors.Wrapf(err, "failed enveloping block %d", block.Number)
			}
			if err := stream.Send(env); err != nil {
				return errors.Wrapf(err, "failed sending block %d", block.Number)
			}
			c.tracker.OnSendTransaction(txID)
		}
		return nil
	})
}

type sidecarTracker struct {
	tracker.ReceiverSender
}

func newSidecarTracker(metrics *PerfMetrics) *sidecarTracker {
	return &sidecarTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *sidecarTracker) OnReceiveSidecarBlock(block *common.Block) {
	statusCodes := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
	logger.Infof("Received block [%d:%d] with %d status codes",
		block.Header.Number, len(block.Data.Data), len(statusCodes))

	for i, data := range block.Data.Data {
		if _, channelHeader, err := serialization.UnwrapEnvelope(data); err == nil {
			t.OnReceiveTransaction(
				channelHeader.TxId, sidecar.StatusInverseMap[statusCodes[i]] == protoblocktx.Status_COMMITTED)
		}
	}
}
