package coordinator

import (
	"context"
	"errors"
	"io"

	errors2 "github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
)

var logger = logging.New("coordinator")

type CoordinatorClient struct {
	client     protocoordinatorservice.CoordinatorClient
	stream     protocoordinatorservice.Coordinator_BlockProcessingClient
	inputChan  <-chan *protoblocktx.Block
	outputChan chan<- *protocoordinatorservice.TxValidationStatusBatch
}

func NewCoordinatorClient(conn grpc.ClientConnInterface) *CoordinatorClient {
	return &CoordinatorClient{
		client: protocoordinatorservice.NewCoordinatorClient(conn),
	}
}

func (c *CoordinatorClient) Start(ctx context.Context, inputChan <-chan *protoblocktx.Block, outputChan chan<- *protocoordinatorservice.TxValidationStatusBatch) (chan error, error) {
	numErrorableGoroutinePerServer := 2
	errChan := make(chan error, numErrorableGoroutinePerServer)

	stream, err := c.client.BlockProcessing(ctx)
	if err != nil {
		// TODO implement retry if needed ...
		return nil, errors2.Wrap(err, "failed to open stream for block processing")
	}

	c.stream = stream
	c.inputChan = inputChan
	c.outputChan = outputChan

	logger.Infof("Starting coordinator sender and receiver")
	go func() {
		errChan <- c.receiveFromCoordinator(ctx)
	}()
	go func() {
		errChan <- c.sendToCoordinator()
	}()

	return errChan, nil
}

func (c *CoordinatorClient) receiveFromCoordinator(ctx context.Context) error {
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

func (c *CoordinatorClient) sendToCoordinator() error {
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

func (c *CoordinatorClient) Close() error {
	close(c.outputChan)
	if err := c.stream.CloseSend(); err != nil {
		return err
	}

	return nil
}
