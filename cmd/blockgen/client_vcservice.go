package main

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type vcClient struct {
	*loadGenClient
	config  *VCClientConfig
	tracker *vcTracker
}

func newVCClient(config *VCClientConfig, metrics *perfMetrics, logger CmdLogger) blockGenClient {
	tracker := newVCTracker(metrics)
	return &vcClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			logger:     logger,
			stopSender: make(chan any, len(config.Endpoints)),
		},
		tracker: tracker,
		config:  config,
	}
}

func (c *vcClient) Start(blockGen *loadgen.BlockStreamGenerator) error {
	stopSender = make(chan any, len(c.config.Endpoints))
	errChan := make(chan error, len(c.config.Endpoints))
	for _, endpoint := range c.config.Endpoints {
		c.logger("Connecting to %s\n", endpoint.String())
		csStream, err := connectToVC(endpoint)
		if err != nil {
			return err
		}

		go func() {
			errChan <- c.startSending(blockGen, csStream)
		}()

		go func() {
			errChan <- c.startReceiving(csStream)
		}()
	}

	c.logger("blockgen started")

	return <-errChan
}

func (c *vcClient) startSending(blockGen *loadgen.BlockStreamGenerator, csStream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient) error {
	return startSendingBlocks(blockGen.BlockQueue, func(block *protoblocktx.Block) error {
		return csStream.Send(mapBatch(block))
	}, c.tracker, c.logger, c.stopSender)
}

func connectToVC(endpoint *connection.Endpoint) (protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient, error) {
	conn, err := connection.Connect(connection.NewDialConfig(*endpoint))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to %s", endpoint.String())
	}

	client := protovcservice.NewValidationAndCommitServiceClient(conn)
	csStream, err := client.StartValidateAndCommitStream(context.Background())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open stream to %s", endpoint.String())
	}
	return csStream, nil
}

func (c *vcClient) startReceiving(stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient) error {
	for {
		if txStatus, err := stream.Recv(); err != nil {
			return err
		} else {
			c.tracker.OnReceiveVCBatch(txStatus)
		}
	}
}

func mapBatch(block *protoblocktx.Block) *protovcservice.TransactionBatch {
	txBatch := &protovcservice.TransactionBatch{}
	for _, tx := range block.Txs {
		txBatch.Transactions = append(
			txBatch.Transactions,
			&protovcservice.Transaction{
				ID:         tx.Id,
				Namespaces: tx.Namespaces,
			},
		)
	}
	return txBatch
}

type vcTracker struct {
	tracker.ReceiverSender
}

func newVCTracker(metrics *perfMetrics) *vcTracker {
	return &vcTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *vcTracker) OnReceiveVCBatch(batch *protovcservice.TransactionStatus) {
	logger.Debugf("Received VC batch with %d items", len(batch.Status))

	for id, status := range batch.Status {
		t.OnReceiveTransaction(id, status == protoblocktx.Status_COMMITTED)
	}
}
