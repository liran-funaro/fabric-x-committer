package sidecar

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/broadcastdeliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/ledger"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/sidecarclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

func init() {
	logging.NoGrpcLog()
}

type sidecarTestEnv struct {
	config      *Config
	coordinator *mock.Coordinator

	sidecar         *Service
	gServer         *grpc.Server
	broadcastClient *broadcastdeliver.EnvelopedStream
	committedBlock  chan *common.Block
}

func newSidecarTestEnv(t *testing.T) *sidecarTestEnv {
	_, orderersServer := mock.StartMockOrderingServices(
		t, 1, mock.OrdererConfig{BlockSize: 100},
	)
	coordinator, coordinatorServer := mock.StartMockCoordinatorService(t)

	env := &sidecarTestEnv{
		coordinator: coordinator,
		config: &Config{
			Server: &connection.ServerConfig{
				Endpoint: connection.Endpoint{
					Host: "localhost",
					Port: 3101,
				},
			},
			Orderer: broadcastdeliver.Config{
				ChannelID: "ch1",
				Endpoints: []*connection.OrdererEndpoint{{
					MspID:    "org",
					Endpoint: orderersServer.Configs[0].Endpoint,
				}},
			},
			Committer: CoordinatorConfig{
				Endpoint: coordinatorServer.Configs[0].Endpoint,
			},
			Ledger: LedgerConfig{
				Path: t.TempDir(),
			},
			Monitoring: &monitoring.Config{
				Metrics: &metrics.Config{
					Enable: true,
					Endpoint: &connection.Endpoint{
						Host: "localhost",
						Port: 3111,
					},
				},
			},
		},
	}

	sidecar, err := New(env.config)
	require.NoError(t, err)
	t.Cleanup(sidecar.Close)
	env.sidecar = sidecar

	broadcastSubmitter, err := broadcastdeliver.New(&broadcastdeliver.Config{
		Endpoints:       env.config.Orderer.Endpoints,
		SignedEnvelopes: false,
		ChannelID:       env.config.Orderer.ChannelID,
		ConsensusType:   broadcastdeliver.Bft,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	env.broadcastClient, err = broadcastSubmitter.Broadcast(ctx)
	require.NoError(t, err)

	return env
}

func (env *sidecarTestEnv) start(t *testing.T, startBlkNum int64) {
	env.gServer = test.RunServiceAndGrpcForTest(t, env.sidecar, env.config.Server, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, env.sidecar.GetLedgerService())
	})

	// we need to wait for the sidecar to connect to ordering service and fetch the
	// config block, i.e., block 0. EnsureAtLeastHeight waits for the block 0 to be committed.
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 1)
	env.committedBlock = sidecarclient.StartSidecarClient(t, &sidecarclient.Config{
		ChannelID: env.config.Orderer.ChannelID,
		Endpoint:  &env.config.Server.Endpoint,
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
			_, resp, err := env.broadcastClient.SubmitWithEnv(protoutil.MarshalOrPanic(txs[i]))
			require.NoError(t, err)
			t.Logf("Response: %s", resp.Info)
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
