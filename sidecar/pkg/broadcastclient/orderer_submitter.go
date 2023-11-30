package broadcastclient

import (
	"context"
	errors2 "errors"

	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
)

var logger = logging.New("broadcastclient")

type Config struct {
	Endpoints         []*connection.Endpoint               `mapstructure:"endpoints"`
	ConnectionProfile *connection.OrdererConnectionProfile `mapstructure:"connection-profile"`
	SignedEnvelopes   bool                                 `mapstructure:"signed-envelopes"`
	Type              utils.ConsensusType                  `mapstructure:"type"`
	ChannelID         string                               `mapstructure:"channel-id"`
	Parallelism       int                                  `mapstructure:"parallelism"`
}

func New(config Config) ([]ab.AtomicBroadcast_BroadcastClient, EnvelopeCreator, error) {
	creds, signer := connection.GetOrdererConnectionCreds(config.ConnectionProfile)
	connections, err := connection.OpenConnections(config.Endpoints, creds)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to open connections to orderers")
	}
	logger.Infof("Opened %d connections to %d orderers", len(connections), len(config.Endpoints))
	streams, err := openClientStreams(connections, config.Type, config.Parallelism)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to open orderer streams")
	}
	logger.Infof("Created %d streams for %d %s orderers (parallelism: %d)", len(streams), len(connections), config.Type, config.Parallelism)

	envelopeCreator := newEnvelopeCreator(config.ChannelID, signer, config.SignedEnvelopes)
	logger.Info("Envelope creator created")
	return streams, envelopeCreator, nil
}

func openClientStreams(connections []*grpc.ClientConn, ordererType utils.ConsensusType, parallelism int) ([]ab.AtomicBroadcast_BroadcastClient, error) {
	if ordererType == utils.Bft && !areEndpointsUnique(connections) {
		return nil, errors.New("bft only supports one connection per orderer")
	}
	streams, err := openStreams(connections, parallelism, ordererType)
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

func openStreams(connections []*grpc.ClientConn, parallelism int, ordererType utils.ConsensusType) ([]ab.AtomicBroadcast_BroadcastClient, error) {
	var streams []ab.AtomicBroadcast_BroadcastClient
	var errs []error
	switch ordererType {
	case utils.Raft:
		streams, errs = openSingleStreams(parallelism, connections)
	case utils.Bft:
		streams, errs = openBroadcastStreams(parallelism, connections)
	default:
		panic("undefined orderer type: " + ordererType)
	}

	if err := errors2.Join(errs...); err != nil {
		return nil, errors.Wrap(err, "failed to open streams")
	}

	return streams, nil
}

func openSingleStreams(parallelism int, connections []*grpc.ClientConn) ([]ab.AtomicBroadcast_BroadcastClient, []error) {
	streams := make([]ab.AtomicBroadcast_BroadcastClient, parallelism*len(connections))
	errs := make([]error, parallelism*len(connections))
	for i := 0; i < parallelism; i++ {
		for j, conn := range connections {
			idx := i*len(connections) + j
			streams[idx], errs[idx] = ab.NewAtomicBroadcastClient(conn).Broadcast(context.Background())
		}
	}
	return streams, errs
}

func openBroadcastStreams(parallelism int, connections []*grpc.ClientConn) ([]ab.AtomicBroadcast_BroadcastClient, []error) {
	streams := make([]ab.AtomicBroadcast_BroadcastClient, parallelism)
	errs := make([]error, parallelism)
	for i := 0; i < parallelism; i++ {
		streams[i], errs[i] = newBroadcastStream(connections)
	}
	return streams, errs
}
