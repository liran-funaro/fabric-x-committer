package loadgen

import (
	"context"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/aggregator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/broadcastclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
)

type sidecarClient struct {
	*loadGenClient
	coordinator     protocoordinatorservice.CoordinatorClient
	envelopeCreator broadcastclient.EnvelopeCreator
	sidecar         deliverclient.Client
	orderers        []ab.AtomicBroadcast_BroadcastClient
	tracker         *sidecarTracker
}

func newSidecarClient(config *SidecarClientConfig, metrics *perfMetrics) blockGenClient {
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

	tracker := newSidecarTracker(metrics)
	return &sidecarClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			stopSender: make(chan any),
		},
		tracker:         tracker,
		coordinator:     coordinatorClient,
		sidecar:         listener,
		orderers:        broadcastClients,
		envelopeCreator: envelopeCreator,
	}
}

func (c *sidecarClient) Stop() {
	logger.Infof("Stopping sidecar client")
	c.sidecar.Stop()
	c.loadGenClient.Stop()
}

func (c *sidecarClient) Start(blockGen *BlockStreamGenerator) error {

	if _, err := c.coordinator.SetVerificationKey(context.Background(), &protosigverifierservice.Key{SerializedBytes: blockGen.Signer.GetVerificationKey()}); err != nil {
		return errors.Wrap(err, "failed connecting to coordinator")
	}
	logger.Infof("Set verification key")

	errChan := make(chan error, len(c.orderers))

	go func() {
		errChan <- c.sidecar.RunDeliverOutputListener(context.Background(), c.tracker.OnReceiveSidecarBlock)
	}()

	for _, stream := range c.orderers {
		go func(stream ab.AtomicBroadcast_BroadcastClient) {
			errChan <- c.startSending(blockGen, stream)
		}(stream)
		go func(stream ab.AtomicBroadcast_BroadcastClient) {
			errChan <- c.startReceiving(stream)
		}(stream)
	}

	return <-errChan
}

func (c *sidecarClient) startReceiving(stream ab.AtomicBroadcast_BroadcastClient) error {
	for {
		if response, err := stream.Recv(); err != nil {
			return errors.Wrapf(err, "failed receiving")
		} else if response.Status != common.Status_SUCCESS {
			return errors.Errorf("unexpected status: %v - %s", response.Status, response.Info)
		} else {
			logger.Debugf("Received ack for TX")
		}
	}
}

func (c *sidecarClient) startSending(blockGen *BlockStreamGenerator, stream ab.AtomicBroadcast_BroadcastClient) error {
	return c.loadGenClient.startSending(blockGen.BlockQueue, stream, func(block *protoblocktx.Block) error {
		logger.Debugf("Sending block %d with %d TXs", block.Number, len(block.Txs))
		for _, tx := range block.Txs {
			env, _, _ := c.envelopeCreator.CreateEnvelope(protoutil.MarshalOrPanic(tx))
			if err := stream.Send(env); err != nil {
				return errors.Wrapf(err, "failed sending block %d", block.Number)
			}
		}
		return nil
	})
}

type sidecarTracker struct {
	tracker.ReceiverSender
}

func newSidecarTracker(metrics *perfMetrics) *sidecarTracker {
	return &sidecarTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *sidecarTracker) OnReceiveSidecarBlock(block *common.Block) {
	statusCodes := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
	logger.Infof("Received block [%d:%d] with %d status codes", block.Header.Number, len(block.Data.Data), len(statusCodes))

	for i, data := range block.Data.Data {
		if payload, _, err := serialization.UnwrapEnvelope(data); err == nil {
			if tx, err := aggregator.UnmarshalTx(payload); err == nil {
				t.OnReceiveTransaction(tx.Id, aggregator.StatusInverseMap[statusCodes[i]] == protoblocktx.Status_COMMITTED)
			}
		}
	}
}
