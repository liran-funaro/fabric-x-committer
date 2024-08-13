package deliverclient

import (
	"context"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
)

var logger = logging.New("deliverlistener")

type ClientProvider interface {
	DeliverClient(conn *grpc.ClientConn) (deliverStream, error)
}

type deliverStream interface {
	Send(*common.Envelope) error
	Recv() (*common.Block, *common.Status, error)
	CloseSend() error
}

type Config struct {
	ChannelID                string                               `mapstructure:"channel-id"`
	Endpoint                 connection.Endpoint                  `mapstructure:"endpoint"`
	OrdererConnectionProfile *connection.OrdererConnectionProfile `mapstructure:"orderer-connection-profile"`
	Reconnect                time.Duration                        `mapstructure:"reconnect"`
}

type Client interface {
	RunDeliverOutputListener(ctx context.Context, onReceive func(*common.Block)) error
	Start(ctx context.Context, outputChan chan<- *common.Block) (chan error, error)
	Stop()
}

func New(config *Config, provider ClientProvider) (*defaultListener, error) {
	if config.Endpoint.Empty() {
		return nil, errors.New("no endpoint passed")
	}
	logger.Infof("Creating listener for %s", config.Endpoint.String())
	creds, signer := connection.GetOrdererConnectionCreds(config.OrdererConnectionProfile)
	conn, err := connection.Connect(connection.NewDialConfigWithCreds(config.Endpoint, creds))
	if err != nil {
		return nil, err
	}
	logger.Infof("Connected to %s", config.Endpoint.String())
	return &defaultListener{
		clientProvider: provider,
		connection:     conn,
		signer:         signer,
		channelID:      config.ChannelID,
		reconnect:      config.Reconnect,
		startBlock:     0,
		stop:           make(chan any),
	}, nil
}
