/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package deliver

import (
	"testing"

	commontypes "github.com/hyperledger/fabric-x-common/api/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/hyperledger/fabric-x-committer/loadgen/workload"
	"github.com/hyperledger/fabric-x-committer/mock"
	"github.com/hyperledger/fabric-x-committer/utils/logging"
	"github.com/hyperledger/fabric-x-committer/utils/signature/sigtest"
)

func BenchmarkVerifyBlock(b *testing.B) {
	logging.SetupWithConfig(&logging.Config{Enabled: false})
	printer := message.NewPrinter(language.English)
	s, p := prepare(b)

	for _, blockSize := range []int{100, 1_000, 10_000, 100_000} {
		b.Run(printer.Sprintf("blockSize=%d", blockSize), func(b *testing.B) {
			txs := workload.GenerateTransactions(b, workload.DefaultProfile(8), b.N)
			blocks := make([]*BlockWithSourceID, 0, 1+(b.N/blockSize))
			for len(txs) > 0 {
				curBlockSize := min(blockSize, len(txs))
				blk := workload.MapToOrdererBlock(0, txs[:curBlockSize])
				txs = txs[curBlockSize:]
				blocks = append(blocks, &BlockWithSourceID{Block: blk})
				mock.PrepareBlockHeaderAndMetadata(blk, p)
				p.PrevBlock = blk
			}

			b.ResetTimer()
			for _, blk := range blocks {
				verErr := s.verificationStepAndUpdateState(blk)
				require.NoError(b, verErr)
				updated, confErr := s.updateIfConfigBlock(blk.Block)
				require.NoError(b, confErr)
				require.False(b, updated)
			}
			b.StopTimer()
		})
	}
}

func TestVerifyBlock(t *testing.T) {
	t.Parallel()
	s, p := prepare(t)

	t.Log("[X] Malformed block")
	txs := workload.GenerateTransactions(t, workload.DefaultProfile(1), 100)
	b1 := workload.MapToOrdererBlock(1, txs)
	err := s.verificationStepAndUpdateState(&BlockWithSourceID{Block: b1})
	require.Error(t, err)
	require.ErrorContains(t, err, "malformed")

	t.Log("[X] Wel-formed block with no signatures")
	mock.PrepareBlockHeaderAndMetadata(b1, mock.BlockPrepareParameters{PrevBlock: p.PrevBlock})
	err = s.verificationStepAndUpdateState(&BlockWithSourceID{Block: b1})
	require.Error(t, err)
	require.ErrorContains(t, err, "block signature verification failed on block")

	t.Log("[V] Well-formed block with signatures")
	mock.PrepareBlockHeaderAndMetadata(b1, p)
	err = s.verificationStepAndUpdateState(&BlockWithSourceID{Block: b1, SourceID: 1})
	require.NoError(t, err)
	require.True(t, s.updated)
	s.updated = false
	require.EqualValues(t, 1, s.updaterID)

	t.Log("[X] Unexpected next block number")
	err = s.verificationStepAndUpdateState(&BlockWithSourceID{Block: b1})
	require.Error(t, err)
	require.ErrorContains(t, err, "received [1] != [2] expected")

	t.Log("[X] Unexpected prev block hash")
	b1a := workload.MapToOrdererBlock(2, txs)
	mock.PrepareBlockHeaderAndMetadata(b1a, mock.BlockPrepareParameters{PrevBlock: p.PrevBlock})
	b1a.Header.Number = 2
	err = s.verificationStepAndUpdateState(&BlockWithSourceID{Block: b1a})
	require.Error(t, err)
	require.ErrorContains(t, err, "previous block header hash mismatch")
	p.PrevBlock = b1

	t.Log("[V] New block")
	submitGoodBlock(t, &s, &p, 5)

	t.Log("[V] New block with minimal quorum")
	minimalQuorum := p
	minimalQuorum.ConsenterSigners = minimalQuorum.ConsenterSigners[:3]
	submitGoodBlock(t, &s, &minimalQuorum, 6)
	p.PrevBlock = minimalQuorum.PrevBlock

	t.Log("[X] New block with insufficient quorum")
	insufficientQuorum := p
	insufficientQuorum.ConsenterSigners = insufficientQuorum.ConsenterSigners[:2]
	b4 := workload.MapToOrdererBlock(1, txs)
	mock.PrepareBlockHeaderAndMetadata(b4, insufficientQuorum)
	err = s.verificationStepAndUpdateState(&BlockWithSourceID{Block: b4})
	require.Error(t, err, "expected error due to insufficient quorum")
	require.ErrorContains(t, err, "2 sub-policies were satisfied, but this policy requires 3")

	t.Log("[V] New Config block")
	_, p2 := prepare(t)
	newConfigBlock := p2.PrevBlock
	mock.PrepareBlockHeaderAndMetadata(newConfigBlock, p)
	err = s.verificationStepAndUpdateState(&BlockWithSourceID{Block: newConfigBlock, SourceID: 2})
	require.NoError(t, err)
	require.True(t, s.updated)
	s.updated = false
	require.EqualValues(t, 2, s.updaterID)
	updated, err := s.updateIfConfigBlock(newConfigBlock)
	require.NoError(t, err)
	require.True(t, updated)

	t.Log("[V] New block with new consenters")
	p2.LastConfigBlockIndex = p2.PrevBlock.Header.Number
	submitGoodBlock(t, &s, &p2, 2)

	t.Log("[X] Block points to older config block")
	p.PrevBlock = p2.PrevBlock
	b7 := workload.MapToOrdererBlock(1, txs)
	mock.PrepareBlockHeaderAndMetadata(b7, p)
	err = s.verificationStepAndUpdateState(&BlockWithSourceID{Block: b7})
	require.Error(t, err, "expected error due to wrong consenters")
	require.ErrorContains(t, err, "block's last config block [4] != [0] actual")

	t.Log("[X] Block with old consenters")
	p.LastConfigBlockIndex = p2.LastConfigBlockIndex
	mock.PrepareBlockHeaderAndMetadata(b7, p)
	err = s.verificationStepAndUpdateState(&BlockWithSourceID{Block: b7})
	require.Error(t, err, "expected error due to wrong consenters")
	require.ErrorContains(t, err, "0 sub-policies were satisfied")
}

func prepare(tb testing.TB) (blockProcessingState, mock.BlockPrepareParameters) {
	tb.Helper()
	cryptoDir := tb.TempDir()
	genesisBlock, err := workload.CreateDefaultConfigBlockWithCrypto(cryptoDir, &workload.ConfigBlock{
		ChannelID: "my-chan",
		OrdererEndpoints: []*commontypes.OrdererEndpoint{
			{ID: 0, Host: "127.0.0.1", Port: 1000},
			{ID: 1, Host: "127.0.0.1", Port: 1001},
			{ID: 2, Host: "127.0.0.1", Port: 1002},
			{ID: 3, Host: "127.0.0.1", Port: 1003},
			{ID: 4, Host: "127.0.0.1", Port: 1004},
		},
	})
	require.NoError(tb, err)

	s := blockProcessingState{dataBlockStream: true, verifyBlocksContent: true}
	updated, err := s.updateIfConfigBlock(genesisBlock)
	require.NoError(tb, err)
	require.True(tb, updated)

	// We process the genesis block first.
	err = s.verificationStepAndUpdateState(&BlockWithSourceID{Block: genesisBlock})
	require.NoError(tb, err)

	identities, idErr := sigtest.GetConsenterIdentities(cryptoDir)
	require.NoError(tb, idErr)
	return s, mock.BlockPrepareParameters{
		LastConfigBlockIndex: 0,
		PrevBlock:            genesisBlock,
		ConsenterSigners:     identities,
	}
}

func submitGoodBlock(
	tb testing.TB, s *blockProcessingState, p *mock.BlockPrepareParameters,
	label uint32,
) {
	tb.Helper()
	txs := workload.GenerateTransactions(tb, workload.DefaultProfile(1), 100)
	b := workload.MapToOrdererBlock(1, txs)
	mock.PrepareBlockHeaderAndMetadata(b, *p)
	err := s.verificationStepAndUpdateState(&BlockWithSourceID{Block: b, SourceID: label})
	require.NoError(tb, err)
	require.True(tb, s.updated)
	s.updated = false
	require.Equal(tb, label, s.updaterID)
	updated, err := s.updateIfConfigBlock(b)
	require.NoError(tb, err)
	require.False(tb, updated)
	p.PrevBlock = b
}
