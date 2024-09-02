package ledger

import (
	"context"
	"testing"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

func TestLedgerService(t *testing.T) {
	ledgerPath := t.TempDir()
	inputBlock := make(chan *common.Block, 10)
	channelID := "ch1"
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ledgerService, err := New(channelID, ledgerPath, inputBlock)
	require.NoError(t, err)

	go func() { require.NoError(t, ledgerService.Run(ctx)) }()

	config := &connection.ServerConfig{
		Endpoint: connection.Endpoint{Host: "localhost"},
	}
	StartGrpcServer(t, config, ledgerService)

	// NOTE: if we start the deliver client without even the 0'th block, it would
	//       result in an error. This is due to the iterator implementation in the
	//       fabric ledger.
	blk0 := test.CreateBlockForTest(nil, 0, nil, [3]string{"0", "1", "2"})
	valid := byte(peer.TxValidationCode_VALID)
	metadata := &common.BlockMetadata{
		Metadata: [][]byte{nil, nil, {valid, valid, valid}},
	}
	blk0.Metadata = metadata
	inputBlock <- blk0

	EnsureHeight(t, ledgerService, 1)

	receivedBlocksFromLedgerService := StartDeliverClient(ctx, t, channelID, config.Endpoint)

	blk1 := test.CreateBlockForTest(nil, 1, protoutil.BlockHeaderHash(blk0.Header), [3]string{"3", "4", "5"})
	blk1.Metadata = metadata
	blk2 := test.CreateBlockForTest(nil, 2, protoutil.BlockHeaderHash(blk1.Header), [3]string{"6", "7", "8"})
	blk2.Metadata = metadata
	inputBlock <- blk1
	inputBlock <- blk2

	for i := range 3 {
		blk := <-receivedBlocksFromLedgerService
		require.Equal(t, uint64(i), blk.Header.Number)
	}
}
