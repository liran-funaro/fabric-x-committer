package sidecar

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters/broadcastclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/ledger"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type sidecarTestEnv struct {
	sidecar         *Service
	coordinator     *mock.Coordinator
	gServer         *grpc.Server
	config          *SidecarConfig
	broadcastClient orderer.AtomicBroadcast_BroadcastClient
	envelopeCreator broadcastclient.EnvelopeCreator
	committedBlock  chan *common.Block
}

func newSidecarTestEnv(t *testing.T) *sidecarTestEnv {
	_, orderersServer := mock.StartMockOrderingServices(
		t, 1, mock.OrdererConfig{BlockSize: 100},
	)
	coordinator, coordinatorServer := mock.StartMockCoordinatorService(t)

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
			Endpoint:  orderersServer.Configs[0].Endpoint,
			Reconnect: -1,
		},
		Committer: &CoordinatorConfig{
			Endpoint: coordinatorServer.Configs[0].Endpoint,
		},
		Ledger: &LedgerConfig{
			Path: t.TempDir(),
		},
	}

	sidecar, err := New(config)
	require.NoError(t, err)
	t.Cleanup(sidecar.Close)

	broadcastSubmitter, err := broadcastclient.New(context.TODO(), broadcastclient.Config{
		Broadcast:       []*connection.Endpoint{&config.Orderer.Endpoint},
		SignedEnvelopes: false,
		ChannelID:       config.Orderer.ChannelID,
		Type:            utils.Raft,
		Parallelism:     1,
	})
	require.NoError(t, err)

	return &sidecarTestEnv{
		sidecar:         sidecar,
		coordinator:     coordinator,
		config:          config,
		broadcastClient: broadcastSubmitter.Streams[0],
		envelopeCreator: broadcastSubmitter.EnvelopeCreator,
	}
}

func (env *sidecarTestEnv) start(t *testing.T, startBlkNum int64) {
	env.gServer = test.RunServiceAndGrpcForTest(t, env.sidecar, env.config.Server, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, env.sidecar.GetLedgerService())
	})

	// we need to wait for the sidecar to connect to ordering service and fetch the
	// config block, i.e., block 0. EnsureAtLeastHeight waits for the block 0 to be committed.
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 1)
	env.committedBlock = ledger.StartDeliverClient(t, &deliverclient.Config{
		ChannelID: env.config.Orderer.ChannelID,
		Endpoint:  env.config.Server.Endpoint,
		Reconnect: -1,
	}, startBlkNum)
}

func TestSidecar(t *testing.T) {
	env := newSidecarTestEnv(t)
	env.start(t, 0)

	// mockorderer expects 100 txs to create the next block
	txs := make([]*protoblocktx.Tx, 100)
	sendTxs := func(txIDPrefix string) {
		for i := range 100 {
			txs[i] = &protoblocktx.Tx{
				Id:         txIDPrefix + strconv.Itoa(i),
				Namespaces: []*protoblocktx.TxNamespace{{NsId: uint32(i)}}, // nolint:gosec
			}
			ev, _, err := env.envelopeCreator.CreateEnvelope(protoutil.MarshalOrPanic(txs[i]))
			require.NoError(t, err)
			require.NoError(t, env.broadcastClient.Send(ev))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	checkLastCommittedBlock(ctx, t, env.coordinator, 0)
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 1)

	sendTxs("tx")
	checkLastCommittedBlock(ctx, t, env.coordinator, 1)
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 2)

	configBlock := <-env.committedBlock
	require.Equal(t, uint64(0), configBlock.Header.Number)

	block := <-env.committedBlock
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

	// restart and ensures it sends the next expected block by the coordinator.
	cancel()
	env.gServer.Stop()
	env.start(t, 2)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel2)

	checkLastCommittedBlock(ctx2, t, env.coordinator, 1)
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 2)
	sendTxs("newTx")
	checkLastCommittedBlock(ctx2, t, env.coordinator, 2)
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 3)
	cancel2()
}

func checkLastCommittedBlock(
	ctx context.Context,
	t *testing.T,
	coordinator *mock.Coordinator,
	expectedBlockNumber uint64,
) {
	require.Eventually(t, func() bool {
		lastBlock, err := coordinator.GetLastCommittedBlockNumber(ctx, nil)
		if err != nil {
			return false
		}
		return expectedBlockNumber == lastBlock.Number
	}, 15*time.Second, 1*time.Second)
}
