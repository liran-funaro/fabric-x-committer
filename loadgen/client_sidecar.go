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
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
)

type sidecarClient struct {
	*loadGenClient
	coordinator     protocoordinatorservice.CoordinatorClient
	envelopeCreator broadcastclient.EnvelopeCreator
	ledgerReceiver  *deliverclient.Receiver
	committedBlock  chan *common.Block
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

	committedBlock := make(chan *common.Block, 100)
	receiver, err := deliverclient.New(&deliverclient.Config{
		ChannelID: config.Orderer.ChannelID,
		Endpoint:  *config.Endpoint,
		Reconnect: -1,
	}, deliverclient.Ledger, committedBlock)
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
		ledgerReceiver:  receiver,
		committedBlock:  committedBlock,
		broadcasts:      broadcastClients,
		delivers:        delivers,
		envelopeCreator: envelopeCreator,
	}
}

func (c *sidecarClient) startWorkload(blockGen *BlockStream) error {
	for _, stream := range c.broadcasts {
		go c.startSending(blockGen, stream)
	}

	for _, stream := range c.delivers {
		go c.startReceiving(stream)
	}

	go c.receiveCommittedBlock()

	return nil
}

func (c *sidecarClient) startNamespaceGeneration(nsGen *NamespaceGenerator) error {
	stream := c.broadcasts[0]

	blkToSend := nsGen.Next()
	logger.Debugf("Sending namaespace-generation-block %d with %d TXs", blkToSend.Number, len(blkToSend.Txs))
	for _, tx := range blkToSend.Txs {
		env, txID, err := c.envelopeCreator.CreateEnvelope(protoutil.MarshalOrPanic(tx))
		utils.Must(err)
		utils.Must(stream.Send(env))
		c.tracker.OnSendTransaction(txID)
	}

	txStatus, err := stream.Recv()
	utils.Must(err)
	if txStatus.Status != common.Status_SUCCESS {
		c.cancel(errors.New("could not commit namespace generation-block"))
	}

	// blocks until ensuring the validation of namespace generation-block
	c.tracker.OnReceiveSidecarBlock(<-c.committedBlock)

	return nil
}

func (c *sidecarClient) Start(blockGen *BlockStream, nsGen *NamespaceGenerator) error {
	if _, err := c.coordinator.SetMetaNamespaceVerificationKey(
		c.ctx,
		&protosigverifierservice.Key{SerializedBytes: nsGen.getSigner().GetVerificationKey()},
	); err != nil {
		return errors.Wrap(err, "failed connecting to coordinator")
	}
	logger.Infof("Set verification key")
	go func() { _ = c.ledgerReceiver.Run(c.ctx) }()

	if err := c.startNamespaceGeneration(nsGen); err != nil {
		return errors.Wrap(err, "failed closing namespace generation connection")
	}

	return c.startWorkload(blockGen)
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

func (c *sidecarClient) startReceiving(stream ab.AtomicBroadcast_BroadcastClient) {
	for c.ctx.Err() == nil {
		response, err := stream.Recv()
		switch {
		case err != nil:
			c.cancel(errors.Wrapf(err, "failed receiving"))
		case response.Status != common.Status_SUCCESS:
			c.cancel(errors.Errorf("unexpected status: %v - %s", response.Status, response.Info))
		default:
			logger.Debugf("Received ack for TX")
		}
	}
}

func (c *sidecarClient) receiveCommittedBlock() {
	for b := range c.committedBlock {
		c.tracker.OnReceiveSidecarBlock(b)
	}
}

func (c *sidecarClient) Stop() {
	logger.Infof("Stopping sidecar client")
	c.loadGenClient.Stop()
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
			t.OnReceiveTransaction(channelHeader.TxId, statusCodes[i] == byte(protoblocktx.Status_COMMITTED))
		}
	}
}
