package main

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type VCClient struct {
	*loadGenClient
	config *VCClientConfig
}

func NewVCClient(config *VCClientConfig, tracker *ClientTracker, logger CmdLogger) LoadGenClient {
	return &VCClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			logger:     logger,
			stopSender: make(chan any, len(config.Endpoints)),
		},
		config: config,
	}
}

func (c *VCClient) Start(blockGen *loadgen.BlockStreamGenerator) error {
	stopSender = make(chan any, len(c.config.Endpoints))
	errChan := make(chan error, len(c.config.Endpoints))
	for _, endpoint := range c.config.Endpoints {
		c.logger("Connecting to %s\n", endpoint.String())
		csStream, err := connectToVC(endpoint)
		if err != nil {
			return err
		}

		go func() {
			errChan <- c.StartSending(blockGen, csStream)
		}()

		go func() {
			errChan <- c.StartReceiving(csStream)
		}()
	}

	c.logger("blockgen started")

	return <-errChan
}

func (c *VCClient) StartSending(blockGen *loadgen.BlockStreamGenerator, csStream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient) error {
	return StartSending(blockGen.BlockQueue, func(block *protoblocktx.Block) error {
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

func (c *VCClient) StartReceiving(stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient) error {
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
