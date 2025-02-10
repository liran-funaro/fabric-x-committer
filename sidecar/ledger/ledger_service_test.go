package ledger

import (
	"context"
	"testing"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/sidecarclient"
	sidecartest "github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

func TestLedgerService(t *testing.T) {
	ledgerPath := t.TempDir()
	inputBlock := make(chan *common.Block, 10)
	channelID := "ch1"

	ledgerService, err := New(channelID, ledgerPath, inputBlock)
	require.NoError(t, err)
	t.Cleanup(ledgerService.Close)

	config := &connection.ServerConfig{
		Endpoint: connection.Endpoint{Host: "localhost"},
	}
	test.RunServiceAndGrpcForTest(context.Background(), t, ledgerService, config, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, ledgerService)
	})

	// NOTE: if we start the deliver client without even the 0'th block, it would
	//       result in an error. This is due to the iterator implementation in the
	//       fabric ledger.
	blk0 := sidecartest.CreateBlockForTest(nil, 0, nil, [3]string{"0", "1", "2"})
	valid := byte(protoblocktx.Status_COMMITTED)
	metadata := &common.BlockMetadata{
		Metadata: [][]byte{nil, nil, {valid, valid, valid}},
	}
	blk0.Metadata = metadata

	require.Zero(t, ledgerService.GetBlockHeight())
	inputBlock <- blk0

	EnsureAtLeastHeight(t, ledgerService, 1)

	receivedBlocksFromLedgerService := sidecarclient.StartSidecarClient(context.Background(), t, &sidecarclient.Config{
		ChannelID: channelID,
		Endpoint:  &config.Endpoint,
	}, 0)

	blk1 := sidecartest.CreateBlockForTest(nil, 1, protoutil.BlockHeaderHash(blk0.Header), [3]string{"3", "4", "5"})
	blk1.Metadata = metadata
	blk2 := sidecartest.CreateBlockForTest(nil, 2, protoutil.BlockHeaderHash(blk1.Header), [3]string{"6", "7", "8"})
	blk2.Metadata = metadata
	inputBlock <- blk1
	inputBlock <- blk2

	EnsureAtLeastHeight(t, ledgerService, 3)
	for i := range 3 {
		blk := <-receivedBlocksFromLedgerService
		require.Equal(t, uint64(i), blk.Header.Number) // nolint:gosec
	}

	// if we input the already stored block, it would simply skip.
	inputBlock <- blk2
	EnsureAtLeastHeight(t, ledgerService, 3)
}
