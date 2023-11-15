package orderer

import (
	"context"
	"io"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/deliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/identity"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
)

var logger = logging.New("orderer")

type OrdererClient struct {
	client        ab.AtomicBroadcastClient
	deliverClient ab.AtomicBroadcast_DeliverClient
	outputChan    chan<- *common.Block

	// config
	clientIdentity identity.SignerSerializer
	channelID      string
	startBlock     int64
}

func NewOrdererClient(conn *grpc.ClientConn, clientIdentity identity.SignerSerializer, channelID string, startBlock int64) *OrdererClient {
	return &OrdererClient{
		client:         ab.NewAtomicBroadcastClient(conn),
		clientIdentity: clientIdentity,
		channelID:      channelID,
		startBlock:     startBlock,
	}
}

func (o *OrdererClient) Start(ctx context.Context, outputChan chan<- *common.Block) (chan error, error) {
	numErrorableGoroutinePerServer := 1
	errChan := make(chan error, numErrorableGoroutinePerServer)

	logger.Infof("Starting deliver client")
	deliverClient, err := o.client.Deliver(ctx)
	if err != nil {
		// TODO implement retry if needed ...
		return nil, errors.Wrap(err, "failed to connect to delivery service")
	}

	o.deliverClient = deliverClient
	o.outputChan = outputChan

	logger.Debugf("Sending seek request starting from block %d", o.startBlock)
	err = o.deliverClient.Send(deliver.SeekSince(o.startBlock, o.channelID, o.clientIdentity))
	if err != nil {
		return nil, errors.Wrap(err, "failed to send seek request")
	}

	logger.Infof("Starting orderer receiver ...")
	go func() {
		errChan <- o.receiveFromOrderer(ctx)
	}()

	return errChan, nil
}

func (o *OrdererClient) receiveFromOrderer(ctx context.Context) error {
	for {
		response, err := o.deliverClient.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return errors.Wrap(err, "failed receiving from delivery service")
		}

		switch t := response.Type.(type) {
		case *ab.DeliverResponse_Status:
			logger.Debugf("Got status %v", t.Status)
		case *ab.DeliverResponse_Block:
			select {
			case <-ctx.Done():
				return nil
			case o.outputChan <- t.Block:
				logger.Debugf("Received block from orderer: %d", t.Block.Header.Number)
			}
		default:
			return errors.New("received unexpected message type from ordering service")
		}
	}
}

func (o *OrdererClient) Close() error {
	logger.Infof("Closing orderer client")
	close(o.outputChan)
	if err := o.deliverClient.CloseSend(); err != nil {
		return err
	}

	return nil
}
