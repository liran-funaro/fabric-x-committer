/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package adapters

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"golang.org/x/sync/errgroup"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/loadgen/workload"
	"github.com/hyperledger/fabric-x-committer/mock"
)

type (
	// SidecarAdapter applies load on the sidecar.
	SidecarAdapter struct {
		commonAdapter
		config *SidecarClientConfig
	}
)

// NewSidecarAdapter instantiate SidecarAdapter.
func NewSidecarAdapter(config *SidecarClientConfig, res *ClientResources) (*SidecarAdapter, error) {
	return &SidecarAdapter{
		res:    res,
		config: config,
	}, nil
}

// RunWorkload applies load on the sidecar.
func (c *SidecarAdapter) RunWorkload(ctx context.Context, txStream *workload.StreamWithSetup) error {
	if len(c.config.OrdererServers) == 0 {
		return errors.New("no orderer servers configured")
	}
	orderer, err := mock.NewMockOrderer(&mock.OrdererConfig{
		Servers:       c.config.OrdererServers,
		ArtifactsPath: c.res.Profile.Policy.ArtifactsPath,
		// The sidecar adapter submits a config block manually.
		SendGenesisBlock: true,
		// This adapter submits blocks it has already cut, so the mock orderer never batches
		// envelopes and BlockSize would only scale the buffer between us and the sidecar
		// (BlockSize * OutBlockCapacity blocks). Pinning it to 1 makes OutBlockCapacity mean
		// what it says: the number of blocks buffered.
		BlockSize:        1,
		OutBlockCapacity: c.config.OutBlockCapacity,
		PrepareInPlace:   c.config.FastBlockPrepare,
	})
	if err != nil {
		return err
	}
	c.NextBlockNum()

	dCtx, dCancel := context.WithCancel(ctx)
	defer dCancel()
	g, gCtx := errgroup.WithContext(dCtx)

	g.Go(func() error {
		return mock.OrdererStartAndServe(gCtx, orderer)
	})

	g.Go(func() error {
		defer dCancel() // We stop sending if we can't track the received items.
		return runSidecarReceiver(gCtx, &sidecarReceiverParameters{
			ClientConfig: c.config.SidecarClient,
			Res:          c.res,
		})
	})
	g.Go(func() error {
		return sendBlocks(
			gCtx, &c.commonAdapter, txStream, c.blockMapper(),
			func(fabricBlock *common.Block) error {
				return orderer.SubmitBlock(gCtx, fabricBlock)
			},
		)
	})
	return errors.Wrap(g.Wait(), "workload done")
}

// blockMapper returns the mapper that assembles a block from a batch of transactions.
//
// With FastBlockPrepare it also hashes the block's data, which is the work this moves. The hash covers
// the block's own data and nothing else, so unlike the block number and the previous hash it does not
// depend on the chain and does not have to be computed in chain order -- and this mapper already runs
// on its own goroutine, one stage ahead of the orderer, doing almost nothing: the transactions arrive
// already serialized, so assembling a block is building a slice of them.
func (c *SidecarAdapter) blockMapper() func(uint64, []*servicepb.LoadGenTx) *common.Block {
	if !c.config.FastBlockPrepare {
		return workload.MapToOrdererBlock
	}
	return func(blockNum uint64, txs []*servicepb.LoadGenTx) *common.Block {
		block := workload.MapToOrdererBlock(blockNum, txs)
		// The orderer overwrites the number when it chains the block, and reads the hash back out.
		block.Header.DataHash = protoutil.ComputeBlockDataHash(block.Data)
		return block
	}
}

// Supports specify which phases an adapter supports.
// The sidecar does not support config transactions as it filters them.
// To generate a config TX, the orderer must submit a config block.
func (*SidecarAdapter) Supports() Phases {
	return Phases{
		Config:     false,
		Namespaces: true,
		Load:       true,
	}
}
