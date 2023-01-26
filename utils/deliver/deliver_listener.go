package deliver

import (
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/identity"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var logger = logging.New("deliverlistener")

type ClientProvider interface {
	DeliverClient(conn *grpc.ClientConn) (Client, error)
}

type Client interface {
	Send(*common.Envelope) error
	Recv() (*common.Block, *common.Status, error)
}

//listener connects to an orderer and listens for one or more blocks
type listener struct {
	clientProvider ClientProvider
	connection     *grpc.ClientConn
	signer         identity.SignerSerializer
	channelID      string
	reconnect      time.Duration
	startBlock     int64
}

type ConnectionOpts struct {
	ClientProvider ClientProvider
	Credentials    credentials.TransportCredentials
	Signer         msp.SigningIdentity
	ChannelID      string
	Endpoint       connection.Endpoint
	Reconnect      time.Duration
	StartBlock     int64
}

func NewListener(opts *ConnectionOpts) (*listener, error) {
	conn, err := connection.Connect(connection.NewDialConfigWithCreds(opts.Endpoint, opts.Credentials))
	if err != nil {
		return nil, err
	}
	return &listener{opts.ClientProvider, conn, opts.Signer, opts.ChannelID, opts.Reconnect, 0}, nil
}

func (l *listener) RunDeliverOutputListener(onReceive func(*common.Block)) error {
	reconnectAttempts := 0
	for {
		err := l.openDeliverStream(onReceive)

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
		return err
	}
	logger.Infof("Opened stream to orderer.")

	logger.Infof("Sending seek request starting from block %d.", l.startBlock)
	err = client.Send(SeekSince(l.startBlock, l.channelID, l.signer))
	if err != nil {
		return err
	}
	logger.Infof("Seek request sent.")

	for {
		block, status, err := client.Recv()
		if err != nil {
			return err
		}
		if status != nil {
			logger.Infof("Got status %v", status)
			return nil
		}
		logger.Infof("Received block %d from orderer", block.Header.Number)
		logger.Debugf("Block: %v", block)
		l.startBlock = int64(block.Header.Number) + 1
		onReceive(block)
	}
}
