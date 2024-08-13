package deliverclient

import (
	"context"
	"io"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/identity"
	"google.golang.org/grpc"
)

type defaultListener struct {
	clientProvider ClientProvider
	connection     *grpc.ClientConn
	signer         identity.SignerSerializer
	channelID      string
	reconnect      time.Duration
	startBlock     int64
	stop           chan any
}

func (l *defaultListener) Start(ctx context.Context, outputChan chan<- *common.Block) (chan error, error) {
	errChan := make(chan error)
	go func() {
		errChan <- l.RunDeliverOutputListener(ctx, func(block *common.Block) {
			outputChan <- block
		})
	}()
	return errChan, nil
}

func (l *defaultListener) Stop() {
	close(l.stop)
}

func (l *defaultListener) RunDeliverOutputListener(ctx context.Context, onReceive func(*common.Block)) error {
	reconnectAttempts := 0
	for {
		errChan, err := l.openDeliverStream(ctx, onReceive)
		if err != nil {
			logger.Warnf("Connection failed ... %v", err)
		}
		err = <-errChan
		logger.Warn("Deliver service disconnected: %v", err)

		if l.reconnect < 0 {
			logger.Infof("Error: %v.\nNot reconnecting.", err)
			return err
		}
		reconnectAttempts++
		logger.Infof("Error: %v.\nReconnecting in %v. Attempt %d", err, l.reconnect, reconnectAttempts)
		<-time.After(l.reconnect)
	}
}

func (l *defaultListener) openDeliverStream(ctx context.Context, onReceive func(*common.Block)) (chan error, error) {
	logger.Infof("Opening stream to orderer: %v", l.connection.Target())
	client, err := l.clientProvider.DeliverClient(l.connection)
	if err != nil {
		return nil, errors.Wrap(err, "failed opening stream")
	}
	defer func() {
		_ = client.CloseSend()
	}()
	logger.Infof("Opened stream to orderer.")

	logger.Infof("Sending seek request starting from block %d.", l.startBlock)
	err = client.Send(seekSince(l.startBlock, l.channelID, l.signer))
	if err != nil {
		return nil, errors.Wrap(err, "failed to send seek request")
	}
	logger.Infof("Seek request sent.")

	errChan := make(chan error)

	go func() {
		errChan <- l.receiveFromOrderer(ctx, onReceive, client)
	}()
	return errChan, nil
}

func (l *defaultListener) receiveFromOrderer(
	ctx context.Context,
	onReceive func(*common.Block),
	client deliverStream,
) error {
	for {
		select {
		case <-l.stop:
		case <-ctx.Done():
			logger.Infof("Stopping listener")
			return nil
		default:
		}

		block, status, err := client.Recv()
		if err != nil || status != nil {
			logger.Warnf("disconnecting deliver service with status %s and error: %s", status, err)
			if errors.Is(err, io.EOF) {
				err = nil
			}
			select {
			case <-l.stop:
			case <-ctx.Done():
				logger.Infof("Stopping listener")
				return nil
			default:
			}
			return err
		}

		logger.Infof("Received block %d from orderer", block.Header.Number)
		select {
		case <-l.stop:
		case <-ctx.Done():
			logger.Infof("Stopping listener")
			return nil
		default:
		}

		l.startBlock = int64(block.Header.Number) + 1
		onReceive(block)
	}
}
