/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sidecar

import (
	"context"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/hyperledger/fabric-x-committer/api/protoblocktx"
	"github.com/hyperledger/fabric-x-committer/service/sidecar/sidecarclient"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

func TestLedgerService(t *testing.T) {
	t.Parallel()
	ledgerPath := t.TempDir()
	channelID := "ch1"

	metrics := newPerformanceMetrics()
	ls, err := newLedgerService(channelID, ledgerPath, metrics)
	require.NoError(t, err)
	t.Cleanup(ls.close)

	config := &connection.ServerConfig{
		Endpoint: connection.Endpoint{Host: "localhost"},
	}

	inputBlock := make(chan *common.Block, 10)
	test.RunServiceForTest(t.Context(), t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(ls.run(ctx, &ledgerRunConfig{
			IncomingCommittedBlock: inputBlock,
		}))
	}, nil)
	test.RunGrpcServerForTest(t.Context(), t, config, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, ls)
	})

	// NOTE: if we start the deliver client without even the 0'th block, it would
	//       result in an error. This is due to the iterator implementation in the
	//       fabric ledger.
	blk0 := createBlockForTest(t, 0, nil, [3]string{"0", "1", "2"})
	valid := byte(protoblocktx.Status_COMMITTED)
	metadata := &common.BlockMetadata{
		Metadata: [][]byte{nil, nil, {valid, valid, valid}},
	}
	blk0.Metadata = metadata

	require.Zero(t, ls.GetBlockHeight())
	inputBlock <- blk0
	ensureAtLeastHeight(t, ls, 1)
	require.Equal(t, 1, test.GetIntMetricValue(t, metrics.blockHeight))
	require.Greater(t, test.GetMetricValue(t, metrics.appendBlockToLedgerSeconds), float64(0))

	receivedBlocksFromLedgerService := sidecarclient.StartSidecarClient(t.Context(), t, &sidecarclient.Config{
		ChannelID: channelID,
		Endpoint:  &config.Endpoint,
	}, 0)

	blk1 := createBlockForTest(t, 1, protoutil.BlockHeaderHash(blk0.Header), [3]string{"3", "4", "5"})
	blk1.Metadata = metadata
	blk2 := createBlockForTest(t, 2, protoutil.BlockHeaderHash(blk1.Header), [3]string{"6", "7", "8"})
	blk2.Metadata = metadata
	inputBlock <- blk1
	inputBlock <- blk2

	ensureAtLeastHeight(t, ls, 3)
	require.Equal(t, 3, test.GetIntMetricValue(t, metrics.blockHeight))
	for i := range 3 {
		blk := <-receivedBlocksFromLedgerService
		require.Equal(t, uint64(i), blk.Header.Number) //nolint:gosec
	}

	// if we input the already stored block, it would simply skip.
	inputBlock <- blk2
	ensureAtLeastHeight(t, ls, 3)
	require.Equal(t, 3, test.GetIntMetricValue(t, metrics.blockHeight))
}

// ensureAtLeastHeight checks if the ledger is at or above the specified height.
func ensureAtLeastHeight(t *testing.T, s *LedgerService, height uint64) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.GreaterOrEqual(ct, s.GetBlockHeight(), height)
	}, 15*time.Second, 10*time.Millisecond)
}
