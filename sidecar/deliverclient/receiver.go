package deliverclient

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/msp"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc/credentials"
)

var logger = logging.New("deliverlistener")

// Sender denotes whether the block sender is a orderer or ledger service.
type Sender int

const (
	// Orderer denotes that the sender of blocks is a ordering service.
	Orderer Sender = iota
	// Ledger denotes that the sender of blocks is a ledger service.
	Ledger
)

type (
	// Receiver receives blocks from either ordering service or ledger service depending on the
	// DeliverStreamProvider.
	Receiver struct {
		sender      Sender
		channelID   string
		reconnect   time.Duration
		startBlock  int64
		outputBlock chan<- *common.Block
		config      *Config
	}

	deliverStream interface {
		Send(*common.Envelope) error
		RecvBlockOrStatus() (*common.Block, *common.Status, error)
		CloseSend() error
	}

	// Config holds the configuration of deliver client receiver.
	Config struct {
		ChannelID                string                               `mapstructure:"channel-id"`
		Endpoint                 connection.Endpoint                  `mapstructure:"endpoint"`
		OrdererConnectionProfile *connection.OrdererConnectionProfile `mapstructure:"orderer-connection-profile"`
		Reconnect                time.Duration                        `mapstructure:"reconnect"`
	}

	// ReceiverRunConfig holds the configuration needed for the receiver to run.
	ReceiverRunConfig struct {
		StartBlkNum int64
	}
)

// New creates a deliver client receiver.
func New(config *Config, sender Sender, outputBlock chan<- *common.Block) (*Receiver, error) {
	if config.Endpoint.Empty() {
		return nil, errors.New("no endpoint passed")
	}

	return &Receiver{
		sender:      sender,
		channelID:   config.ChannelID,
		reconnect:   config.Reconnect,
		outputBlock: outputBlock,
		config:      config,
	}, nil
}

// Run starts the block receiver. The call to Run blocks until an error occurs or the context is canceled.
func (l *Receiver) Run(ctx context.Context, config *ReceiverRunConfig) error {
	l.startBlock = config.StartBlkNum
	rCtx, rCancel := context.WithCancel(ctx)
	defer rCancel()

	reconnectAttempts := 0
	for {
		err := l.receiveFromBlockDeliverer(rCtx)
		if err == nil {
			return nil
		}

		if l.reconnect < 0 {
			logger.Infof("Error: %v.\nNot reconnecting.", err)
			return err
		}
		reconnectAttempts++
		logger.Infof("Error: %v.\nReconnecting in %v. Attempt %d", err, l.reconnect, reconnectAttempts)

		select {
		case <-time.After(l.reconnect):
		case <-rCtx.Done():
			return nil
		}
	}
}

func (l *Receiver) receiveFromBlockDeliverer(ctx context.Context) error {
	var stream deliverStream
	var err error
	var creds credentials.TransportCredentials
	var signer msp.SigningIdentity

	switch l.sender {
	case Orderer:
		creds, signer = connection.GetOrdererConnectionCreds(l.config.OrdererConnectionProfile)
		conn, connErr := connection.Connect(connection.NewDialConfigWithCreds(l.config.Endpoint, creds))
		if connErr != nil {
			return connErr
		}
		defer conn.Close() // nolint:errcheck
		stream, err = newOrdererDeliverStream(ctx, conn)
	case Ledger:
		conn, connErr := connection.Connect(connection.NewDialConfig(l.config.Endpoint))
		if connErr != nil {
			return connErr
		}
		defer conn.Close() // nolint:errcheck
		stream, err = newLedgerDeliverStream(ctx, conn)
	default:
		return fmt.Errorf("unknown sender type: [%v]", l.sender)
	}
	if err != nil {
		return err
	}

	logger.Infof("Sending seek request starting from block %d on channel %s.", l.startBlock, l.channelID)
	if err := stream.Send(seekSince(l.startBlock, l.channelID, signer)); err != nil {
		return errors.Wrap(err, "failed to send seek request")
	}
	logger.Infof("Seek request sent.")

	outputBlock := channel.NewWriter(ctx, l.outputBlock)
	for ctx.Err() == nil {
		block, status, err := stream.RecvBlockOrStatus()
		if err != nil || status != nil {
			logger.Warnf("disconnecting deliver service with status %s and error: %s", status, err)
			return connection.FilterStreamErrors(err)
		}

		l.startBlock = int64(block.Header.Number) + 1
		logger.Infof("next expected block number is %d", l.startBlock)
		outputBlock.Write(block)
	}

	return nil
}
