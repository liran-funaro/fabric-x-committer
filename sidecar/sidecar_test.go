package sidecar

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/broadcastdeliver"
	configtempl "github.ibm.com/decentralized-trust-research/scalable-committer/config/templates"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/ledger"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/sidecarclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/proto"
)

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
		t, &mock.OrdererConfig{NumService: 1, BlockSize: 100, BlockTimeout: time.Minute},
	)
	coordinator, coordinatorServer := mock.StartMockCoordinatorService(t)

	return &sidecarTestEnv{
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
}

func (env *sidecarTestEnv) init(t *testing.T) {
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
}

func (env *sidecarTestEnv) start(ctx context.Context, t *testing.T, startBlkNum int64) {
	env.gServer = test.RunServiceAndGrpcForTest(ctx, t, env.sidecar, env.config.Server, func(server *grpc.Server) {
		peer.RegisterDeliverServer(server, env.sidecar.GetLedgerService())
	})

	// we need to wait for the sidecar to connect to ordering service and fetch the
	// config block, i.e., block 0. EnsureAtLeastHeight waits for the block 0 to be committed.
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 1)
	env.committedBlock = sidecarclient.StartSidecarClient(ctx, t, &sidecarclient.Config{
		ChannelID: env.config.Orderer.ChannelID,
		Endpoint:  &env.config.Server.Endpoint,
	}, startBlkNum)
}

func TestSidecar(t *testing.T) {
	commonTest(t, newSidecarTestEnv(t))
}

func commonTest(t *testing.T, env *sidecarTestEnv) {
	env.init(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t, 0)

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
	require.Eventually(t, func() bool {
		return checkServerStopped(t, env.config.Server.Endpoint.Address())
	}, 4*time.Second, 500*time.Millisecond)
	require.Eventually(t, func() bool {
		return !env.coordinator.IsStreamActive()
	}, 2*time.Second, 250*time.Millisecond)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel2)
	env.start(ctx2, t, 2)

	checkLastCommittedBlock(ctx2, t, env.coordinator, 1)
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 2)
	sendTxs("newTx")

	block = <-env.committedBlock
	require.Equal(t, uint64(2), block.Header.Number)

	checkLastCommittedBlock(ctx2, t, env.coordinator, 2)
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 3)
	cancel2()
}

func makeBlock(t *testing.T, env *sidecarTestEnv) {
	configBlockPath := configtempl.CreateConfigBlock(t, &configtempl.ConfigBlock{
		ChannelID:        env.config.Orderer.ChannelID,
		OrdererEndpoints: env.config.Orderer.Endpoints,
	})
	cfg := configtempl.SidecarConfig{
		CommonEndpoints: configtempl.CommonEndpoints{
			ServerEndpoint:  env.config.Server.Endpoint.String(),
			MetricsEndpoint: env.config.Monitoring.Metrics.Endpoint.String(),
		},
		ChannelID:           env.config.Orderer.ChannelID,
		CoordinatorEndpoint: env.config.Committer.Endpoint.String(),
		LedgerPath:          env.config.Ledger.Path,
		ConfigBlockPath:     configBlockPath,
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configtempl.CreateConfigFile(t, cfg, "../config/templates/sidecar.yaml", configPath)
	require.NoError(t, config.ReadYamlConfigs([]string{configPath}))

	conf := ReadConfig()
	env.config = &conf
}

func TestSidecarWithBlock(t *testing.T) {
	env := newSidecarTestEnv(t)
	makeBlock(t, env)
	commonTest(t, env)
}

func TestSidecarWithBlockMultipleWrongOrderers(t *testing.T) {
	env := newSidecarTestEnv(t)

	var id uint32
	var msp string
	for _, e := range env.config.Orderer.Endpoints {
		id = e.ID
		msp = e.MspID
	}

	for range 3 {
		fakeServer := &connection.ServerConfig{Endpoint: connection.Endpoint{Host: "localhost"}}
		healthcheck := health.NewServer()
		healthcheck.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)
		test.RunGrpcServerForTest(context.Background(), t, fakeServer, func(server *grpc.Server) {
			healthgrpc.RegisterHealthServer(server, healthcheck)
		})
		env.config.Orderer.Endpoints = append(env.config.Orderer.Endpoints, &connection.OrdererEndpoint{
			ID:       id,
			MspID:    msp,
			API:      []string{connection.Broadcast, connection.Deliver},
			Endpoint: fakeServer.Endpoint,
		})
	}
	t.Log(env.config.Orderer.Endpoints)
	makeBlock(t, env)
	t.Log(env.config.Orderer.Endpoints)
	commonTest(t, env)
}

func TestSidecarMetaNamespaceVerificationKey(t *testing.T) {
	env := newSidecarTestEnv(t)
	makeBlock(t, env)
	p := env.config.Policies
	require.NotNil(t, p)
	require.Len(t, p.Policies, 1)
	require.Equal(t, types.MetaNamespaceID.Bytes(), p.Policies[0].Namespace)
	nsp := protoblocktx.NamespacePolicy{}
	require.NoError(t, proto.Unmarshal(p.Policies[0].Policy, &nsp))
	verifier, err := signature.NewNsVerifier(nsp.Scheme, nsp.PublicKey)
	require.NoError(t, err)
	require.NotNil(t, verifier)
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

func checkServerStopped(_ *testing.T, addr string) bool {
	// Try a short timeout so we don't block too long
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	conn, err := grpc.DialContext( // nolint:staticcheck
		ctx,
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(), // nolint:staticcheck
	)
	if err != nil {
		// If we cannot connect at all, the server is likely stopped.
		fmt.Println("Connection failed as expected:", err)
		return true
	}
	// If we get a connection object, it means something is listening on that port.
	_ = conn.Close()
	return false
}
