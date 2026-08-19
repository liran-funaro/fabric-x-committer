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
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-common/common/ledger/blkstorage"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/delivercommitter"
	"github.com/hyperledger/fabric-x-committer/utils/serve"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

type blockDeliveryTestWrapper struct {
	*Service
}

func (w *blockDeliveryTestWrapper) RegisterService(s serve.Servers) {
	peer.RegisterDeliverServer(s.GRPC, w)
}

func TestBlockStoreAndDelivery(t *testing.T) {
	t.Parallel()
	ledgerPath := t.TempDir()

	metrics := newPerformanceMetrics(newQueues(10))
	bs, err := newBlockStore(&LedgerConfig{Path: ledgerPath}, metrics)
	require.NoError(t, err)
	t.Cleanup(bs.close)

	bd := &blockDeliveryTestWrapper{Service: &Service{blockStore: bs, metrics: metrics}}

	serverConfig := test.NewLocalHostServiceConfig(test.InsecureTLSConfig)
	inputBlock := make(chan *common.Block, 10)
	test.RunServiceForTest(t.Context(), t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(bs.run(ctx, &blockStoreRunConfig{
			IncomingCommittedBlock: inputBlock,
		}))
	}, nil)
	test.ServeForTest(t.Context(), t, serverConfig, bd)

	// NOTE: if we start the delivery client without even the 0'th block, it would
	//       result in an error. This is due to the iterator implementation in the
	//       fabric ledger.
	blk0, _ := createBlockForTest(t, 0, nil)
	valid := byte(committerpb.Status_COMMITTED)
	metadata := &common.BlockMetadata{
		Metadata: [][]byte{nil, nil, {valid, valid, valid}},
	}
	blk0.Metadata = metadata

	require.Zero(t, bs.GetBlockHeight())
	inputBlock <- blk0
	ensureAtLeastHeight(t, bs, 1)
	require.Equal(t, 1, test.GetIntMetricValue(t, metrics.blockHeight))
	require.Greater(t, test.GetMetricValue(t, metrics.appendBlockToLedgerSeconds), float64(0))

	committerClient := test.NewInsecureClientConfig(&serverConfig.GRPC.Endpoint)
	receivedBlocksFromLedgerService := delivercommitter.Start(t.Context(), t, committerClient, 0)

	blk1, _ := createBlockForTest(t, 1, protoutil.BlockHeaderHash(blk0.Header))
	blk1.Metadata = metadata
	blk2, _ := createBlockForTest(t, 2, protoutil.BlockHeaderHash(blk1.Header))
	blk2.Metadata = metadata
	inputBlock <- blk1
	inputBlock <- blk2

	ensureAtLeastHeight(t, bs, 3)
	require.Equal(t, 3, test.GetIntMetricValue(t, metrics.blockHeight))
	for i := range 3 {
		blk := <-receivedBlocksFromLedgerService
		require.Equal(t, uint64(i), blk.Header.Number) //nolint:gosec
	}

	// if we input the already stored block, it would simply skip.
	inputBlock <- blk2
	ensureAtLeastHeight(t, bs, 3)
	require.Equal(t, 3, test.GetIntMetricValue(t, metrics.blockHeight))

	// TODO: appendBlock forces fsync (Append) for single-tx blocks since they may be config blocks,
	//       but we cannot verify Append vs AppendNoSync here because the ledger field is a concrete
	//       *fileledger.FileLedger (not an interface). To properly test this, the ledger dependency
	//       would need to be behind an interface so we can assert which method was called.
}

func TestLedgerIndexedAttrs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		disableTxID bool
		expected    []blkstorage.IndexableAttr
	}{
		{
			// The zero value must keep the transaction ID index, since a config built in Go
			// rather than read from YAML would otherwise silently drop it.
			name: "default indexes everything",
			expected: []blkstorage.IndexableAttr{
				blkstorage.IndexableAttrBlockNum,
				blkstorage.IndexableAttrTxID,
			},
		},
		{
			name:        "transaction ID index disabled",
			disableTxID: true,
			expected:    []blkstorage.IndexableAttr{blkstorage.IndexableAttrBlockNum},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			attrs := indexedAttrs(&LedgerConfig{
				Path:             t.TempDir(),
				DisableTxIDIndex: tc.disableTxID,
			})
			require.Equal(t, tc.expected, attrs)
		})
	}
}

// TestBlockStoreWithoutIndexes covers the reason the indexes are configurable: appending a
// block must not depend on either index, so a deployment that serves no block or
// transaction query can drop the per-transaction index work. The queries that do need an
// index have to fail rather than return a wrong answer.
func TestBlockStoreWithoutIndexes(t *testing.T) {
	t.Parallel()

	metrics := newPerformanceMetrics(newQueues(10))
	bs, err := newBlockStore(&LedgerConfig{
		Path:             t.TempDir(),
		DisableTxIDIndex: true,
	}, metrics)
	require.NoError(t, err)
	t.Cleanup(bs.close)

	inputBlock := make(chan *common.Block, 10)
	test.RunServiceForTest(t.Context(), t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(bs.run(ctx, &blockStoreRunConfig{
			IncomingCommittedBlock: inputBlock,
		}))
	}, nil)

	valid := byte(committerpb.Status_COMMITTED)
	metadata := &common.BlockMetadata{
		Metadata: [][]byte{nil, nil, {valid, valid, valid}},
	}
	blk0, txIDs := createBlockForTest(t, 0, nil)
	blk0.Metadata = metadata
	blk1, _ := createBlockForTest(t, 1, protoutil.BlockHeaderHash(blk0.Header))
	blk1.Metadata = metadata

	inputBlock <- blk0
	inputBlock <- blk1
	ensureAtLeastHeight(t, bs, 2)
	require.Equal(t, 2, test.GetIntMetricValue(t, metrics.blockHeight))

	// The height comes from the block file info rather than the index, so it stays correct.
	require.Equal(t, uint64(2), bs.GetBlockHeight())

	// The block number index is always maintained, so this query keeps working.
	_, err = bs.store.RetrieveBlockByNumber(0)
	require.NoError(t, err)
	_, err = bs.store.RetrieveBlockByTxID(txIDs[0])
	require.Error(t, err)
}

// TestBlockStoreReopenWithoutTxIDIndex covers sidecar recovery, which reopens a non-empty
// ledger. This is the case that decided the block number index cannot be made optional: the
// block store reads the last block header through it on open, so dropping it panics here
// while dropping the transaction ID index does not.
func TestBlockStoreReopenWithoutTxIDIndex(t *testing.T) {
	t.Parallel()

	config := &LedgerConfig{Path: t.TempDir(), DisableTxIDIndex: true}
	bs, err := newBlockStore(config, newPerformanceMetrics(newQueues(10)))
	require.NoError(t, err)

	inputBlock := make(chan *common.Block, 10)
	test.RunServiceForTest(t.Context(), t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(bs.run(ctx, &blockStoreRunConfig{
			IncomingCommittedBlock: inputBlock,
		}))
	}, nil)

	valid := byte(committerpb.Status_COMMITTED)
	blk0, _ := createBlockForTest(t, 0, nil)
	blk0.Metadata = &common.BlockMetadata{Metadata: [][]byte{nil, nil, {valid, valid, valid}}}
	inputBlock <- blk0
	ensureAtLeastHeight(t, bs, 1)
	bs.close()

	reopened, err := newBlockStore(config, newPerformanceMetrics(newQueues(10)))
	require.NoError(t, err)
	t.Cleanup(reopened.close)
	require.Equal(t, uint64(1), reopened.GetBlockHeight())
}

// ensureAtLeastHeight checks if the ledger is at or above the specified height.
func ensureAtLeastHeight(t *testing.T, s *blockStore, height uint64) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.GreaterOrEqual(ct, s.GetBlockHeight(), height)
	}, 15*time.Second, 10*time.Millisecond)
}
