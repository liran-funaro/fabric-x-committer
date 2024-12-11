package sidecar

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/coordinatormock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
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
	relay := newRelay(
		&CoordinatorConfig{Endpoint: coordinatorServerConfig.Endpoint},
		uncommittedBlock,
		committedBlock,
	)

	conn, err := connection.Connect(connection.NewDialConfig(coordinatorServerConfig.Endpoint))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	logger.Infof("sidecar connected to coordinator at %s", &coordinatorServerConfig.Endpoint)

	client := protocoordinatorservice.NewCoordinatorClient(conn)

	wg := &sync.WaitGroup{}
	// NOTE: we should cancel the context before waiting for the completion. Therefore, the
	//       order of cleanup matters, which is last added first called.
	t.Cleanup(wg.Wait)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	wg.Add(1)
	go func() {
		require.NoError(t, connection.FilterStreamErrors(relay.Run(ctx, &relayRunConfig{client, 0})))
		wg.Done()
	}()
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
	valid := byte(protoblocktx.Status_COMMITTED)
	expectedMetadata := &common.BlockMetadata{
		Metadata: [][]byte{nil, nil, {valid, valid, valid}},
	}
	require.Equal(t, expectedMetadata, committedBlock0.Metadata)
	require.Equal(t, blk0, committedBlock0)
}

func TestBlockWithDuplicateTransactions(t *testing.T) {
	relayEnv := newRelayTestEnv(t)
	blk0 := test.CreateBlockForTest(nil, 0, nil, [3]string{"tx1", "tx1", "tx1"})
	require.Nil(t, blk0.Metadata)
	relayEnv.uncommittedBlock <- blk0
	committedBlock0 := <-relayEnv.committedBlock
	valid := byte(protoblocktx.Status_COMMITTED)
	duplicate := byte(protoblocktx.Status_ABORTED_DUPLICATE_TXID)
	expectedMetadata := &common.BlockMetadata{
		Metadata: [][]byte{nil, nil, {valid, duplicate, duplicate}},
	}
	require.Equal(t, expectedMetadata, committedBlock0.Metadata)
	require.Equal(t, blk0, committedBlock0)

	blk1 := test.CreateBlockForTest(nil, 1, nil, [3]string{"tx2", "tx3", "tx2"})
	require.Nil(t, blk1.Metadata)
	relayEnv.uncommittedBlock <- blk1
	committedBlock1 := <-relayEnv.committedBlock
	expectedMetadata = &common.BlockMetadata{
		Metadata: [][]byte{nil, nil, {valid, valid, duplicate}},
	}
	require.Equal(t, expectedMetadata, committedBlock1.Metadata)
	require.Equal(t, blk1, committedBlock1)
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
