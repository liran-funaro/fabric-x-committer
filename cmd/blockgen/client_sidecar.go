package main

import (
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/aggregator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/ordererclient"
	"google.golang.org/grpc/credentials/insecure"
)

type sidecarClient struct {
	*loadGenClient
	config  *SidecarClientConfig
	tracker *sidecarTracker
}

func newSidecarClient(config *SidecarClientConfig, metrics *perfMetrics, logger CmdLogger) blockGenClient {
	tracker := newSidecarTracker(metrics)
	return &sidecarClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			logger:     logger,
			stopSender: make(chan any),
		},
		tracker: tracker,
		config:  config,
	}
}

func (c *sidecarClient) Start(blockGen *loadgen.BlockStreamGenerator) error {
	creds, signer := connection.GetOrdererConnectionCreds(c.config.Orderer.ConnectionProfile)

	if _, err := connectToCoordinator(*c.config.Coordinator.Endpoint, blockGen.Signer.GetVerificationKey()); err != nil {
		return errors.Wrap(err, "failed connecting to coordinator")
	}

	ordererClient, err := ordererclient.NewClient(&ordererclient.ClientInitOptions{
		SignedEnvelopes: c.config.Orderer.SignedEnvelopes,
		OrdererSigner:   signer,

		OrdererEndpoints:   c.config.Orderer.Endpoints,
		OrdererCredentials: creds,

		DeliverEndpoint:       c.config.Endpoint,
		DeliverCredentials:    insecure.NewCredentials(),
		DeliverSigner:         nil,
		DeliverClientProvider: &orderer.PeerDeliverClientProvider{},

		ChannelID:   c.config.Orderer.ChannelID,
		Parallelism: c.config.Orderer.Parallelism,
		OrdererType: c.config.Orderer.Type,
		StartBlock:  0,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create orderer client")
	}

	ordererClient.Start(messageGenerator(blockGen.BlockQueue), c.tracker.OnSendTransaction, c.tracker.OnReceiveSidecarBlock)

	return nil
}

func messageGenerator(blocks <-chan *protoblocktx.Block) <-chan []byte {
	messages := make(chan []byte, 100)
	for workerID := 0; workerID < 10; workerID++ {
		go func() {
			for block := range blocks {
				for _, tx := range block.Txs {
					messages <- protoutil.MarshalOrPanic(tx)
				}
			}
		}()
	}
	return messages
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
