package broadcastclient

import (
	"context"
	errors2 "errors"

	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
)

var logger = logging.New("broadcastclient")

type (
	// Config for the broadcast client.
	Config struct {
		Broadcast         []*connection.Endpoint               `mapstructure:"broadcast"`
		Deliver           []*connection.Endpoint               `mapstructure:"deliver"`
		ConnectionProfile *connection.OrdererConnectionProfile `mapstructure:"connection-profile"`
		SignedEnvelopes   bool                                 `mapstructure:"signed-envelopes"`
		Type              utils.ConsensusType                  `mapstructure:"type"`
		ChannelID         string                               `mapstructure:"channel-id"`
		Parallelism       int                                  `mapstructure:"parallelism"`
	}

	// Submitter is a collection of streams and their connections.
	Submitter struct {
		Streams         []ab.AtomicBroadcast_BroadcastClient
		EnvelopeCreator EnvelopeCreator
		connections     []*grpc.ClientConn
	}
)

// New creates submitter streams. It must be closed to close all the associated connections.
func New(ctx context.Context, config Config) (*Submitter, error) {
	creds, signer := connection.GetOrdererConnectionCreds(config.ConnectionProfile)
	connections, err := connection.OpenConnections(config.Broadcast, creds)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open connections to orderers")
	}
	logger.Infof("Opened %d connections to %d orderers", len(connections), len(config.Broadcast))
	streams, err := openClientStreams(ctx, config.Parallelism, connections, config.Type)
	if err != nil {
		connection.CloseConnectionsLog(connections...)
		return nil, errors.Wrap(err, "failed to open orderer streams")
	}
	logger.Infof(
		"Created %d streams for %d %s orderers (parallelism: %d)",
		len(streams), len(connections), config.Type, config.Parallelism,
	)

	return &Submitter{
		Streams:         streams,
		EnvelopeCreator: newEnvelopeCreator(config.ChannelID, signer, config.SignedEnvelopes),
		connections:     connections,
	}, nil
}

// Close closes all the connections for the submitter streams.
func (s *Submitter) Close() {
	connection.CloseConnectionsLog(s.connections...)
}

func openClientStreams(
	ctx context.Context,
	parallelism int,
	connections []*grpc.ClientConn,
	ordererType utils.ConsensusType,
) ([]ab.AtomicBroadcast_BroadcastClient, error) {
	if ordererType == utils.Bft && !areEndpointsUnique(connections) {
		return nil, errors.New("bft only supports one connection per orderer")
	}
	streams, err := openStreams(ctx, parallelism, connections, ordererType)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open streams")
	}
	return streams, nil
}

func areEndpointsUnique(connections []*grpc.ClientConn) bool {
	uniqueEndpoints := make(map[string]bool, len(connections))
	for _, conn := range connections {
		target := conn.Target()
		if uniqueEndpoints[target] {
			return false
		}
		uniqueEndpoints[target] = true
	}
	return true
}

func openStreams(
	ctx context.Context,
	parallelism int,
	connections []*grpc.ClientConn,
	ordererType utils.ConsensusType,
) ([]ab.AtomicBroadcast_BroadcastClient, error) {
	var streams []ab.AtomicBroadcast_BroadcastClient
	var errs []error
	switch ordererType {
	case utils.Raft:
		streams, errs = openSingleStreams(ctx, parallelism, connections)
	case utils.Bft:
		streams, errs = openBroadcastStreams(ctx, parallelism, connections)
	default:
		panic("undefined orderer type: " + ordererType)
	}

	if err := errors2.Join(errs...); err != nil {
		return nil, errors.Wrap(err, "failed to open streams")
	}

	return streams, nil
}

func openSingleStreams(
	ctx context.Context,
	parallelism int,
	connections []*grpc.ClientConn,
) ([]ab.AtomicBroadcast_BroadcastClient, []error) {
	streams := make([]ab.AtomicBroadcast_BroadcastClient, parallelism*len(connections))
	errs := make([]error, parallelism*len(connections))
	for i := 0; i < parallelism; i++ {
		for j, conn := range connections {
			idx := i*len(connections) + j
			streams[idx], errs[idx] = ab.NewAtomicBroadcastClient(conn).Broadcast(ctx)
		}
	}
	return streams, errs
}

func openBroadcastStreams(
	ctx context.Context,
	parallelism int,
	connections []*grpc.ClientConn,
) ([]ab.AtomicBroadcast_BroadcastClient, []error) {
	streams := make([]ab.AtomicBroadcast_BroadcastClient, parallelism)
	errs := make([]error, parallelism)
	for i := 0; i < parallelism; i++ {
		streams[i], errs[i] = newBroadcastStream(ctx, connections)
	}
	return streams, errs
}
