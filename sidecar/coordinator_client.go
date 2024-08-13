package sidecar

import (
	"context"
	"errors"
	"io"

	errors2 "github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

type coordinatorClient struct {
	client     protocoordinatorservice.CoordinatorClient
	stream     protocoordinatorservice.Coordinator_BlockProcessingClient
	inputChan  chan *protoblocktx.Block
	outputChan chan<- *protocoordinatorservice.TxValidationStatusBatch
}

func newCoordinatorClient(config *CoordinatorConfig) (*coordinatorClient, error) {
	logger.Infof("Create coordinator client and connect to %v\n", config.Endpoint)
	coordinatorConn, err := connection.Connect(connection.NewDialConfig(config.Endpoint))
	if err != nil {
		return nil, errors2.Wrap(err, "failed to connect to coordinator")
	}
	return newClient(coordinatorConn), nil
}

func newClient(conn grpc.ClientConnInterface) *coordinatorClient {
	return &coordinatorClient{
		client:    protocoordinatorservice.NewCoordinatorClient(conn),
		inputChan: make(chan *protoblocktx.Block, 100),
	}
}

func (c *coordinatorClient) Input() chan<- *protoblocktx.Block {
	return c.inputChan
}

func (c *coordinatorClient) Start(
	ctx context.Context,
	outputChan chan<- *protocoordinatorservice.TxValidationStatusBatch,
) (chan error, error) {
	stream, err := c.client.BlockProcessing(ctx)
	if err != nil {
		// TODO implement retry if needed ...
		return nil, errors2.Wrap(err, "failed to open stream for block processing")
	}

	c.stream = stream
	c.outputChan = outputChan

	logger.Infof("Starting coordinator sender and receiver")
	numErrorableGoroutinePerServer := 2
	errChan := make(chan error, numErrorableGoroutinePerServer)
	go func() {
		errChan <- c.receiveFromCoordinator(ctx)
	}()
	go func() {
		errChan <- c.sendToCoordinator()
	}()

	return errChan, nil
}

func (c *coordinatorClient) receiveFromCoordinator(ctx context.Context) error {
	defer close(c.outputChan)
	for {
		response, err := c.stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return errors2.Wrap(err, "failed to receive from coordinator")
		}
		logger.Debugf("Received status batch (%d updates) from coordinator", len(response.GetTxsValidationStatus()))

		select {
		case <-ctx.Done():
			return nil
		case c.outputChan <- response:
		}
	}
}

func (c *coordinatorClient) sendToCoordinator() error {
	for block := range c.inputChan {
		if err := c.stream.SendMsg(block); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return errors2.Wrap(err, "failed to send to coordinator")
		}
		logger.Debugf("Sent scBlock %d with %d transactions to Coordinator", block.Number, len(block.Txs))
	}

	return nil
}

func (c *coordinatorClient) Close() error {
	return c.stream.CloseSend()
}
