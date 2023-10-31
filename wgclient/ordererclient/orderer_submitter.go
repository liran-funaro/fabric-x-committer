package ordererclient

import (
	"context"
	errors2 "errors"

	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
)

var logger = logging.New("ordererclient")

func OpenBroadcastStreams(connections []*grpc.ClientConn, ordererType utils.ConsensusType, parallelism int) ([]ab.AtomicBroadcast_BroadcastClient, error) {
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
	errs := make([]error, parallelism)
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
		streams[i], errs[i] = NewBroadcastStream(connections)
	}
	return streams, errs
}
