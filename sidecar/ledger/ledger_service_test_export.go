package ledger

import (
	"context"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

// StartDeliverClient starts a deliver client to fetch committed blocks from the ledger service.
func StartDeliverClient(
	t *testing.T,
	config *deliverclient.Config,
	startBlkNum int64,
) chan *common.Block {
	receivedBlocksFromLedgerService := make(chan *common.Block, 10)
	deliverClient, err := deliverclient.New(config, deliverclient.Ledger, receivedBlocksFromLedgerService)
	require.NoError(t, err)
	test.RunServiceForTest(t, func(ctx context.Context) error {
		return connection.WrapStreamRpcError(deliverClient.Run(ctx,
			&deliverclient.ReceiverRunConfig{StartBlkNum: startBlkNum},
		))
	}, nil)
	return receivedBlocksFromLedgerService
}

// EnsureAtLeastHeight checks if the ledger is at or above the specified height.
func EnsureAtLeastHeight(t *testing.T, s *Service, height uint64) {
	require.Eventually(t, func() bool {
		return s.ledger.Height() >= height
	}, 5*time.Second, 500*time.Millisecond)
}
