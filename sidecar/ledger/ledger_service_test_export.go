package ledger

import (
	"context"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

// StartGrpcServer starts a grpc server for the ledger service.
func StartGrpcServer(_ *testing.T, config *connection.ServerConfig, ledgerService *Service) *grpc.Server {
	var s *grpc.Server
	connection.RunServerMainAndWait(config, func(server *grpc.Server, port int) {
		if config.Endpoint.Port == 0 {
			config.Endpoint.Port = port
		}
		s = server
		peer.RegisterDeliverServer(server, ledgerService)
	})

	return s
}

// StartDeliverClient starts a deliver client to fetch committed blocks from the ledger service.
func StartDeliverClient(
	ctx context.Context,
	t *testing.T,
	config *deliverclient.Config,
	startBlkNum int64,
) chan *common.Block {
	receivedBlocksFromLedgerService := make(chan *common.Block, 10)
	deliverClient, err := deliverclient.New(config, deliverclient.Ledger, receivedBlocksFromLedgerService)
	require.NoError(t, err)

	go func() {
		require.NoError(t, connection.FilterStreamErrors(deliverClient.Run(ctx,
			&deliverclient.ReceiverRunConfig{StartBlkNum: startBlkNum},
		)))
	}()
	return receivedBlocksFromLedgerService
}

// EnsureAtLeastHeight checks if the ledger is at or above the specified height.
func EnsureAtLeastHeight(t *testing.T, s *Service, height uint64) {
	require.Eventually(t, func() bool {
		return s.ledger.Height() >= height
	}, 5*time.Second, 500*time.Millisecond)
}
