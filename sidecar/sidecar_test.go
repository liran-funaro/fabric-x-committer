package sidecar

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/coordinatormock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/broadcastclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/ledger"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/orderermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"google.golang.org/protobuf/proto"
)

func TestSidecar(t *testing.T) {
	orderersServerConfig, orderers, orderersGrpcServer := orderermock.StartMockOrderingServices(1, nil, 100, 0)
	t.Cleanup(func() {
		for _, o := range orderers {
			o.Close()
		}
		for _, oGrpc := range orderersGrpcServer {
			oGrpc.Stop()
		}
	})

	coordinatorServerConfig, coordinator, coordGrpcServer := coordinatormock.StartMockCoordinatorService()
	t.Cleanup(func() {
		coordinator.Close()
		coordGrpcServer.Stop()
	})

	channelID := "ch1"
	config := &SidecarConfig{
		Server: &connection.ServerConfig{
			Endpoint: connection.Endpoint{
				Host: "localhost",
				Port: 3101,
			},
		},
		Orderer: &deliverclient.Config{
			ChannelID: channelID,
			Endpoint:  orderersServerConfig[0].Endpoint,
			Reconnect: -1,
		},
		Committer: &CoordinatorConfig{
			Endpoint: coordinatorServerConfig.Endpoint,
		},
		Ledger: &LedgerConfig{
			Path: t.TempDir(),
		},
	}

	sidecar, err := New(config)
	require.NoError(t, err)

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	wg.Add(1)
	go func() { require.NoError(t, connection.FilterStreamErrors(sidecar.Run(ctx))); wg.Done() }()
	ledger.StartGrpcServer(t, config.Server, sidecar.GetLedgerService())

	broadcastClients, envelopeCreator, err := broadcastclient.New(broadcastclient.Config{
		Broadcast:       []*connection.Endpoint{&orderersServerConfig[0].Endpoint},
		SignedEnvelopes: false,
		ChannelID:       channelID,
		Type:            utils.Raft,
		Parallelism:     1,
	})
	require.NoError(t, err)
	// when we start the broadcastclient, it would pull the config block
	// from the orderer and commit it.
	ledger.EnsureHeight(t, sidecar.GetLedgerService(), 1)
	checkLastCommittedBlock(ctx, t, coordinator, 0)

	// orderermock expects 100 txs to create the next block
	txs := make([]*protoblocktx.Tx, 100)
	for i := range 100 {
		txs[i] = &protoblocktx.Tx{
			Id:         "tx" + strconv.Itoa(i),
			Namespaces: []*protoblocktx.TxNamespace{{NsId: uint32(i)}},
		}
		env, _, err := envelopeCreator.CreateEnvelope(protoutil.MarshalOrPanic(txs[i]))
		require.NoError(t, err)
		require.NoError(t, broadcastClients[0].Send(env))
	}
	ledger.EnsureHeight(t, sidecar.GetLedgerService(), 2)
	checkLastCommittedBlock(ctx, t, coordinator, 1)

	committedBlock := ledger.StartDeliverClient(ctx, t, channelID, config.Server.Endpoint)
	configBlock := <-committedBlock
	require.Equal(t, uint64(0), configBlock.Header.Number)

	block := <-committedBlock
	require.Equal(t, uint64(1), block.Header.Number)

	for i := range 100 {
		actualEnv, err := protoutil.ExtractEnvelope(block, i)
		require.NoError(t, err)
		payload, _, err := serialization.ParseEnvelope(actualEnv)
		require.NoError(t, err)
		tx, err := UnmarshalTx(payload.Data)
		require.NoError(t, err)
		require.True(t, proto.Equal(txs[i], tx))
	}
}

func checkLastCommittedBlock(
	ctx context.Context,
	t *testing.T,
	coordinator *coordinatormock.MockCoordinator,
	expectedBlockNumber uint64,
) {
	require.Eventually(t, func() bool {
		lastBlock, err := coordinator.GetLastCommittedBlockNumber(ctx, nil)
		if err != nil {
			return false
		}
		return expectedBlockNumber == lastBlock.Number
	}, 4*time.Second, 1*time.Second)
}
