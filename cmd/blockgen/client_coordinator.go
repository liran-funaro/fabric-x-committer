package main

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type CoordinatorClient struct {
	*loadGenClient
	config *CoordinatorClientConfig
}

func NewCoordinatorClient(config *CoordinatorClientConfig, tracker *ClientTracker, logger CmdLogger) LoadGenClient {
	return &CoordinatorClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			logger:     logger,
			stopSender: make(chan any),
		},
		config: config,
	}
}

func (c *CoordinatorClient) Start(blockGen *loadgen.BlockStreamGenerator) error {
	stopSender = make(chan any)
	client, err := connectToCoordinator(*c.config.Endpoint, blockGen.Signer.GetVerificationKey())
	if err != nil {
		return errors.Wrap(err, "connection to coordinator failed")
	}

	csStream, err := client.BlockProcessing(context.Background())
	if err != nil {
		return errors.Wrap(err, "failed creating stream to coordinator")
	}

	errChan := make(chan error)
	go func() {
		errChan <- StartSending(blockGen.BlockQueue, csStream.Send, c.tracker.senderTracker, c.logger, c.stopSender)
	}()
	go func() {
		errChan <- StartReceiving(csStream, c.tracker)
	}()
	return <-errChan
}

func connectToCoordinator(endpoint connection.Endpoint, publicKey signature.PublicKey) (protocoordinatorservice.CoordinatorClient, error) {
	conn, err := connection.Connect(connection.NewDialConfig(endpoint))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to establish connection with %s", endpoint.String())
	}

	client := protocoordinatorservice.NewCoordinatorClient(conn)

	_, err = client.SetVerificationKey(
		context.Background(),
		&protosigverifierservice.Key{SerializedBytes: publicKey},
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed setting verification key")
	}
	return client, nil
}

func StartReceiving(stream protocoordinatorservice.Coordinator_BlockProcessingClient, c *ClientTracker) error {
	for {
		if txStatus, err := stream.Recv(); err != nil {
			return errors.Wrap(err, "failed receiving tx")
		} else {
			c.OnReceiveCoordinatorBatch(txStatus)
		}
	}
}
