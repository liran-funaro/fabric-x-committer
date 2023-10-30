package main

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type coordinatorClient struct {
	*loadGenClient
	config  *CoordinatorClientConfig
	tracker *coordinatorTracker
}

func newCoordinatorClient(config *CoordinatorClientConfig, metrics *perfMetrics, logger CmdLogger) blockGenClient {
	tracker := newCoordinatorTracker(metrics)
	return &coordinatorClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			logger:     logger,
			stopSender: make(chan any),
		},
		tracker: tracker,
		config:  config,
	}
}

func (c *coordinatorClient) Start(blockGen *loadgen.BlockStreamGenerator) error {
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
		errChan <- startSendingBlocks(blockGen.BlockQueue, csStream.Send, c.tracker, c.logger, c.stopSender)
	}()
	go func() {
		errChan <- c.startReceiving(csStream)
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

func (c *coordinatorClient) startReceiving(stream protocoordinatorservice.Coordinator_BlockProcessingClient) error {
	for {
		if txStatus, err := stream.Recv(); err != nil {
			return errors.Wrap(err, "failed receiving tx")
		} else {
			c.tracker.OnReceiveCoordinatorBatch(txStatus)
		}
	}
}

type coordinatorTracker struct {
	tracker.ReceiverSender
}

func newCoordinatorTracker(metrics *perfMetrics) *coordinatorTracker {
	return &coordinatorTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *coordinatorTracker) OnReceiveCoordinatorBatch(batch *protocoordinatorservice.TxValidationStatusBatch) {
	logger.Debugf("Received coordinator batch with %d items", len(batch.TxsValidationStatus))

	for _, tx := range batch.TxsValidationStatus {
		t.OnReceiveTransaction(tx.TxId, tx.Status == protoblocktx.Status_COMMITTED)
	}
}
