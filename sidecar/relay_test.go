package sidecar

import (
	"context"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/coordinatormock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/test"
)

type relayTestEnv struct {
	relay            *relay
	uncommittedBlock chan *common.Block
	committedBlock   chan *common.Block
}

func newRelayTestEnv(t *testing.T) *relayTestEnv {
	coordinatorServerConfig, mockCoordinator, coordinatorGrpc := coordinatormock.StartMockCoordinatorService()
	t.Cleanup(func() {
		coordinatorGrpc.Stop()
		mockCoordinator.Close()
	})

	uncommittedBlock := make(chan *common.Block, 10)
	committedBlock := make(chan *common.Block, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	relay := newRelay(
		&CoordinatorConfig{Endpoint: coordinatorServerConfig.Endpoint},
		uncommittedBlock,
		committedBlock,
	)

	go func() { require.NoError(t, relay.Run(ctx)) }()

	return &relayTestEnv{
		relay:            relay,
		uncommittedBlock: uncommittedBlock,
		committedBlock:   committedBlock,
	}
}

func TestRelayNormalBlock(t *testing.T) {
	relayEnv := newRelayTestEnv(t)
	blk0 := test.CreateBlockForTest(nil, 0, nil, [3]string{"tx1", "tx2", "tx3"})
	require.Nil(t, blk0.Metadata)
	relayEnv.uncommittedBlock <- blk0
	committedBlock0 := <-relayEnv.committedBlock
	valid := byte(peer.TxValidationCode_VALID)
	expectedMetadata := &common.BlockMetadata{
		Metadata: [][]byte{nil, nil, {valid, valid, valid}},
	}
	require.Equal(t, expectedMetadata, committedBlock0.Metadata)
	require.Equal(t, blk0, committedBlock0)
}

func TestRelayConfigBlock(t *testing.T) {
	relayEnv := newRelayTestEnv(t)
	configBlk := createConfigBlockForTest(nil, 0)
	relayEnv.uncommittedBlock <- configBlk
	committedBlock := <-relayEnv.committedBlock
	require.Equal(t, configBlk, committedBlock)
}

func createConfigBlockForTest(_ *testing.T, number uint64) *common.Block {
	data := protoutil.MarshalOrPanic(&common.Envelope{
		Payload: protoutil.MarshalOrPanic(&common.Payload{
			Header: &common.Header{
				ChannelHeader: protoutil.MarshalOrPanic(&common.ChannelHeader{
					Type: int32(common.HeaderType_CONFIG),
				}),
			},
		},
		),
	},
	)

	return &common.Block{
		Header: &common.BlockHeader{Number: number},
		Data:   &common.BlockData{Data: [][]byte{data}},
	}
}
