package deliver

import (
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/identity"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var logger = logging.New("deliverlistener")

type ClientProvider interface {
	DeliverClient(conn *grpc.ClientConn) (Client, error)
}

type Client interface {
	Send(*common.Envelope) error
	Recv() (*common.Block, *common.Status, error)
	CloseSend() error
}

// listener connects to an orderer and listens for one or more blocks
type listener struct {
	clientProvider ClientProvider
	connection     *grpc.ClientConn
	signer         identity.SignerSerializer
	channelID      string
	reconnect      time.Duration
	startBlock     int64
	stop           chan any
}

type Options struct {
	ClientProvider ClientProvider
	Credentials    credentials.TransportCredentials
	Signer         msp.SigningIdentity
	ChannelID      string
	Endpoint       *connection.Endpoint
	Reconnect      time.Duration
	StartBlock     int64
}

type noopListener struct{}

func (l *noopListener) RunDeliverOutputListener(func(*common.Block)) error { return nil }
func (l *noopListener) Stop()                                              {}

type OrdererListener interface {
	RunDeliverOutputListener(onReceive func(*common.Block)) error
	Stop()
}

func NewDefaultListener(endpoint *connection.Endpoint, channelID string) (OrdererListener, error) {
	return NewListener(&Options{
		ClientProvider: &PeerDeliverClientProvider{},
		Credentials:    insecure.NewCredentials(),
		Signer:         nil,
		ChannelID:      channelID,
		Endpoint:       endpoint,
		Reconnect:      -1,
		StartBlock:     0,
	})
}

func NewListener(opts *Options) (OrdererListener, error) {
	if opts.Endpoint == nil {
		logger.Info("Creating no-op listener")
		return &noopListener{}, nil
	}
	logger.Infof("Creating listener for %s", opts.Endpoint.String())
	conn, err := connection.Connect(connection.NewDialConfigWithCreds(*opts.Endpoint, opts.Credentials))
	if err != nil {
		return nil, err
	}
	logger.Infof("Connected to %s", opts.Endpoint.String())
	return &listener{
		clientProvider: opts.ClientProvider,
		connection:     conn,
		signer:         opts.Signer,
		channelID:      opts.ChannelID,
		reconnect:      opts.Reconnect,
		startBlock:     opts.StartBlock,
		stop:           make(chan any),
	}, nil
}

func (l *listener) Stop() {
	close(l.stop)
}

func (l *listener) RunDeliverOutputListener(onReceive func(*common.Block)) error {
	reconnectAttempts := 0
	for {
		err := l.openDeliverStream(onReceive)
		if err != nil {
			logger.Warnf("Connection failed ... %v", err)
		}

		if l.reconnect < 0 {
			logger.Infof("Error: %v.\nNot reconnecting.", err)
			return err
		} else {
			reconnectAttempts += 1
			logger.Infof("Error: %v.\nReconnecting in %v. Attempt %d", err, l.reconnect, reconnectAttempts)
			<-time.After(l.reconnect)
		}
	}
}

func (l *listener) openDeliverStream(onReceive func(*common.Block)) error {
	logger.Infof("Opening stream to orderer: %v", l.connection.Target())
	client, err := l.clientProvider.DeliverClient(l.connection)
	if err != nil {
		return errors.Wrap(err, "failed opening stream")
	}
	defer client.CloseSend()
	logger.Infof("Opened stream to orderer.")

	logger.Infof("Sending seek request starting from block %d.", l.startBlock)
	err = client.Send(SeekSince(l.startBlock, l.channelID, l.signer))
	if err != nil {
		return errors.Wrap(err, "failed to send seek request")
	}
	logger.Infof("Seek request sent.")

	for {
		select {
		case <-l.stop:
			logger.Infof("Stopping listener")
			return nil
		default:
		}

		block, status, err := client.Recv()
		if err != nil || status != nil {
			return utils.ErrorUnlessStopped(l.stop, err)
		}
		logger.Infof("Received block %d from orderer", block.Header.Number)
		logger.Debugf("Block: %v", block)
		l.startBlock = int64(block.Header.Number) + 1
		onReceive(block)
	}
}
