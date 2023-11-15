package main

import (
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/ordererclient"
	"google.golang.org/grpc/credentials/insecure"
)

type sidecarClient struct {
	*loadGenClient
	config *SidecarClientConfig
}

func newSidecarClient(config *SidecarClientConfig, tracker *ClientTracker, logger CmdLogger) blockGenClient {
	return &sidecarClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			logger:     logger,
			stopSender: make(chan any),
		},
		config: config,
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

	ordererClient.Start(messageGenerator(blockGen.BlockQueue), c.tracker.OnSendTransaction, c.tracker.OnReceiveBlock)

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
