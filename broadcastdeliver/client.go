package broadcastdeliver

import (
	"context"
	"fmt"

	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

type (
	// Client is a collection of nodes and their connections.
	Client struct {
		config      *Config
		connections []*ordererConnection
		signer      protoutil.Signer
	}
)

// New creates a broadcast/deliver client. It must be closed to release all the associated connections.
func New(config *Config) (*Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	creds, credSigner := GetOrdererConnectionCreds(config.ConnectionProfile)
	var signer protoutil.Signer
	if config.SignedEnvelopes {
		signer = credSigner
	}

	s := &Client{
		config:      config,
		connections: make([]*ordererConnection, len(config.Endpoints)),
		signer:      signer,
	}

	conn, err := connection.OpenLazyConnections(config.Endpoints, creds)
	if err != nil {
		return nil, fmt.Errorf("failed to open connections to ordering services: %w", err)
	}
	logger.Infof("Opened %d connections", len(conn))
	for i := range s.connections {
		s.connections[i] = &ordererConnection{
			OrdererEndpoint: config.Endpoints[i],
			ClientConn:      conn[i],
		}
	}
	return s, nil
}

// Close closes all the connections for the client.
func (s *Client) Close() {
	connection.CloseConnectionsLog(s.connections...)
}

// Broadcast creates a broadcast stream.
func (s *Client) Broadcast(ctx context.Context) (*EnvelopedStream, error) {
	ret := &EnvelopedStream{
		channelID: s.config.ChannelID,
		signer:    s.signer,
	}
	switch s.config.ConsensusType {
	case Cft:
		ret.BroadcastStream = newBroadcastCft(ctx, s.connections, s.config)
	case Bft:
		ret.BroadcastStream = newBroadcastBft(ctx, s.connections, s.config)
	default:
		return nil, fmt.Errorf("invalid consensus type: %s", s.config.ConsensusType)
	}
	return ret, nil
}

// Deliver starts the block receiver. The call to Deliver blocks until an error occurs or the context is canceled.
func (s *Client) Deliver(ctx context.Context, deliverConfig *DeliverConfig) error {
	switch s.config.ConsensusType {
	case Bft:
		// TODO: We should borrow Fabric's delivery client implementation that supports BFT.
		logger.Warnf("deliver consensus type %s is not supported; falling back to %s",
			s.config.ConsensusType, Cft)
		fallthrough
	case Cft:
		c := &DeliverCftClient{
			Connections: filterOrdererConnections(s.connections, Deliver),
			Signer:      s.signer,
			Retry:       s.config.Retry,
			ChannelID:   s.config.ChannelID,
			StreamCreator: func(ctx context.Context, conn grpc.ClientConnInterface) (DeliverStream, error) {
				client, err := orderer.NewAtomicBroadcastClient(conn).Deliver(ctx)
				if err != nil {
					return nil, err
				}
				return &ordererDeliverStream{client}, nil
			},
		}
		return c.Deliver(ctx, deliverConfig)
	default:
		return fmt.Errorf("invalid consensus type: %s", s.config.ConsensusType)
	}
}
