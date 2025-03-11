package sidecar

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric/common/ledger/blkstorage"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/broadcastdeliver"
	configtempl "github.ibm.com/decentralized-trust-research/scalable-committer/config/templates"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/ledger"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/sidecarclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type sidecarTestEnv struct {
	config           Config
	coordinator      *mock.Coordinator
	orderer          *mock.Orderer
	ordererServers   *test.GrpcServers
	ordererEndpoints []*connection.OrdererEndpoint
	chanID           string

	sidecar        *Service
	gServer        *grpc.Server
	committedBlock chan *common.Block
}

type sidecarTestConfig struct {
	NumService      int
	NumFakeService  int
	WithConfigBlock bool
}

const (
	blockSize              = 100
	expectedProcessingTime = 15 * time.Second
)

func (c *sidecarTestConfig) String() string {
	var b []string
	if c.NumService > 1 {
		b = append(b, fmt.Sprintf("services:%d", c.NumService))
	}
	if c.NumFakeService > 0 {
		b = append(b, fmt.Sprintf("fake:%d", c.NumFakeService))
	}
	if c.WithConfigBlock {
		b = append(b, "config-block")
	}
	if len(b) > 0 {
		return strings.Join(b, ",")
	}
	return "default"
}

func newSidecarTestEnv(t *testing.T, conf sidecarTestConfig) *sidecarTestEnv {
	t.Helper()
	coordinator, coordinatorServer := mock.StartMockCoordinatorService(t)
	orderer, ordererServers := mock.StartMockOrderingServices(
		t,
		&mock.OrdererConfig{
			NumService: conf.NumService,
			BlockSize:  blockSize,
			// We want each block to contain exactly <blockSize> transactions.
			// Therefore, we set a higher block timeout so that we have enough time to send all the
			// transactions to the orderer and create a block.
			BlockTimeout:    5 * time.Minute,
			SendConfigBlock: false,
		})
	fakeConfigs := make([]*connection.ServerConfig, conf.NumFakeService)
	for i := range fakeConfigs {
		fakeConfigs[i] = &connection.ServerConfig{Endpoint: connection.Endpoint{Host: "localhost"}}
		test.RunGrpcServerForTest(t.Context(), t, fakeConfigs[i])
	}
	ordererEndpoints := append(
		connection.NewOrdererEndpoints(0, "org", ordererServers.Configs...),
		connection.NewOrdererEndpoints(0, "org", fakeConfigs...)...,
	)

	chanID := "ch1"
	configBlock, err := workload.CreateDefaultConfigBlock(&configtempl.ConfigBlock{
		ChannelID:        chanID,
		OrdererEndpoints: ordererEndpoints,
	})
	require.NoError(t, err)
	orderer.SubmitBlock(t.Context(), configBlock)

	var genesisBlockFilePath string
	initOrdererEndpoints := ordererEndpoints
	if conf.WithConfigBlock {
		genesisBlockFilePath = configtempl.WriteConfigBlock(t, configBlock)
		initOrdererEndpoints = nil
	}
	sidecarConf := &Config{
		Server: &connection.ServerConfig{
			Endpoint: connection.Endpoint{Host: "localhost"},
		},
		Orderer: broadcastdeliver.Config{
			ChannelID: chanID,
			Connection: broadcastdeliver.ConnectionConfig{
				Endpoints: initOrdererEndpoints,
			},
		},
		Committer: CoordinatorConfig{
			Endpoint: coordinatorServer.Configs[0].Endpoint,
		},
		Ledger: LedgerConfig{
			Path: t.TempDir(),
		},
		LastCommittedBlockSetInterval: 100 * time.Millisecond,
		Bootstrap: Bootstrap{
			GenesisBlockFilePath: genesisBlockFilePath,
		},
	}
	sidecar, err := New(sidecarConf)
	require.NoError(t, err)
	t.Cleanup(sidecar.Close)

	return &sidecarTestEnv{
		sidecar:          sidecar,
		coordinator:      coordinator,
		orderer:          orderer,
		ordererServers:   ordererServers,
		ordererEndpoints: ordererEndpoints,
		chanID:           chanID,
		config:           *sidecarConf,
	}
}

func (env *sidecarTestEnv) start(ctx context.Context, t *testing.T, startBlkNum int64) {
	t.Helper()
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
	t.Parallel()
	for _, conf := range []sidecarTestConfig{
		{WithConfigBlock: false},
		{WithConfigBlock: true},
		{
			WithConfigBlock: true,
			NumFakeService:  3,
		},
	} {
		conf := conf
		t.Run(conf.String(), func(t *testing.T) {
			t.Parallel()
			env := newSidecarTestEnv(t, conf)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			t.Cleanup(cancel)
			env.start(ctx, t, 0)
			env.requireBlock(ctx, t, 0)
			sendTransactionsAndEnsureCommitted(ctx, t, env, 1)
		})
	}
}

func TestSidecarConfigUpdate(t *testing.T) {
	t.Parallel()
	env := newSidecarTestEnv(t, sidecarTestConfig{NumService: 3})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t, 0)
	env.requireBlock(ctx, t, 0)

	// Sanity check
	expectedBlock := uint64(1)
	sendTransactionsAndEnsureCommitted(ctx, t, env, expectedBlock)
	expectedBlock++

	// Update the sidecar to use a second orderer group.
	holder := &mock.HoldingOrderer{
		Orderer: env.orderer,
	}
	holder.HoldBlock.Store(expectedBlock + 2)
	holderServers := test.StartGrpcServersForTest(
		ctx, t, 3, func(server *grpc.Server, _ int) {
			ab.RegisterAtomicBroadcastServer(server, holder)
		},
	)

	submitConfigBlock := func(endpoints []*connection.OrdererEndpoint) {
		configBlock, err := workload.CreateDefaultConfigBlock(&workload.ConfigBlock{
			ChannelID:        env.chanID,
			OrdererEndpoints: endpoints,
		})
		require.NoError(t, err)
		env.orderer.SubmitBlock(ctx, configBlock)
	}
	submitConfigBlock(connection.NewOrdererEndpoints(0, "org", holderServers.Configs...))
	env.requireBlock(ctx, t, expectedBlock)
	expectedBlock++

	// Sanity check
	sendTransactionsAndEnsureCommitted(ctx, t, env, expectedBlock)
	expectedBlock++

	// We submit the config that returns to the non-holding orderer.
	// But it should not be processed as the sidecar should have switched to the holding
	// orderer.
	submitConfigBlock(env.ordererEndpoints)
	select {
	case <-ctx.Done():
		t.Fatal("context deadline exceeded")
	case <-env.committedBlock:
		t.Fatal("the sidecar cannot receive blocks since its orderer holds them")
	case <-time.After(expectedProcessingTime):
		// Fantastic.
	}

	// We expect the block to be held.
	lastBlock, err := env.coordinator.GetLastCommittedBlockNumber(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, expectedBlock-1, lastBlock.Number)

	// We advance the holder by one to allow the config block to pass through, but not other blocks.
	holder.HoldBlock.Add(1)
	env.requireBlock(ctx, t, expectedBlock)
	expectedBlock++
	// The sidecar should use the non-holding orderer, so the holding should not affect the processing.
	sendTransactionsAndEnsureCommitted(ctx, t, env, expectedBlock)
}

func TestSidecarConfigRecovery(t *testing.T) {
	t.Parallel()
	env := newSidecarTestEnv(t, sidecarTestConfig{NumService: 3})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t, 0)
	env.requireBlock(ctx, t, 0)
	// TODO: implement
}

func TestSidecarRecovery(t *testing.T) {
	t.Parallel()
	env := newSidecarTestEnv(t, sidecarTestConfig{})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t, 0)
	env.requireBlock(ctx, t, 0)

	t.Log("1. Commit block 1 to 10")
	for i := range 10 {
		sendTransactionsAndEnsureCommitted(ctx, t, env, uint64(i+1)) //nolint:gosec
	}

	t.Log("2. Stop the sidecar service and ledger service")
	cancel()
	require.Eventually(t, func() bool {
		return test.CheckServerStopped(t, env.config.Server.Endpoint.Address())
	}, 4*time.Second, 500*time.Millisecond)
	require.Eventually(t, func() bool {
		return !env.coordinator.IsStreamActive()
	}, 2*time.Second, 250*time.Millisecond)

	// NOTE: It is challenging to make the block store lag behind the state
	//       database because we record the last committed block number in
	//       the database at regular intervals (e.g., every five seconds).
	//       In most cases, this interval allows the sidecar enough time
	//       to commit the block to the block store. However, if the sidecar
	//       fails immediately after recording the last committed block
	//       number—i.e., before the ledger server empties its input queues—
	//       then the block store could end up behind the state database.
	//       Directly testing this scenario could lead to flakiness.
	//       Therefore, to simulate it, we use certain Fabric utilities
	//       to artificially create a situation where the block store
	//       falls behind the state database by removing blocks.
	t.Log("3. Remove all blocks from the ledger except block 0")
	require.NoError(t, blkstorage.ResetBlockStore(env.config.Ledger.Path))
	ctx2, cancel2 := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel2)
	checkLastCommittedBlock(ctx2, t, env.coordinator, 10)

	// NOTE: The Fabric block store implementation used by the sidecar
	//       maintains the ledger height in memory and updates it whenever
	//       a new block is committed. Utilities such as ResetBlockStore
	//       are expected to be executed only when the process is fully
	//       shut down. Consequently, when the block store object is
	//       recreated, it reads the current height from disk. Therefore,
	//       we reset the ledger service object so that its in-memory data
	//       structure reflects the correct height.
	var err error
	env.sidecar.ledgerService, err = ledger.New(
		env.config.Orderer.ChannelID,
		env.config.Ledger.Path,
		env.sidecar.committedBlock,
	)
	require.NoError(t, err)
	ledger.EnsureAtLeastHeight(t, env.sidecar.ledgerService, 1) // back to block 0

	t.Log("4. Make coordinator not idle to ensure sidecar is waiting")
	env.coordinator.SetWaitingTxsCount(10)

	t.Log("5. Restart the sidecar service and ledger service")
	env.start(ctx2, t, 11)

	t.Log("6. Recovery should not happen when coordinator is not idle.")
	require.Never(t, func() bool {
		return env.sidecar.ledgerService.GetBlockHeight() > 1
	}, 3*time.Second, 1*time.Second)

	t.Log("7. Make coordinator idle")
	env.coordinator.SetWaitingTxsCount(0)

	t.Log("8. Blockstore would recover lost blocks")
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 11)

	t.Log("9. Send the next expected block by the coordinator.")
	sendTransactionsAndEnsureCommitted(ctx2, t, env, 11)

	checkLastCommittedBlock(ctx2, t, env.coordinator, 11)
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), 12)
	cancel2()
}

func sendTransactionsAndEnsureCommitted(
	ctx context.Context,
	t *testing.T,
	env *sidecarTestEnv,
	expectedBlockNumber uint64,
) {
	t.Helper()
	txIDPrefix := uuid.New().String()
	// mock-orderer expects <blockSize> txs to create the next block.
	txs := make([]*protoblocktx.Tx, blockSize)
	for i := range txs {
		txs[i] = &protoblocktx.Tx{
			Id:         txIDPrefix + strconv.Itoa(i),
			Namespaces: []*protoblocktx.TxNamespace{{NsId: strconv.Itoa(i)}},
		}
		_, err := env.orderer.SubmitPayload(ctx, env.chanID, txs[i])
		require.NoErrorf(t, err, "block %d, tx %d", expectedBlockNumber, i)
	}
	block := env.requireBlock(ctx, t, expectedBlockNumber)
	require.NotNil(t, block.Data)
	require.Len(t, block.Data.Data, blockSize)
	for i := range block.Data.Data {
		actualEnv, err := protoutil.ExtractEnvelope(block, i)
		require.NoError(t, err)
		payload, _, err := serialization.ParseEnvelope(actualEnv)
		require.NoError(t, err)
		tx, err := serialization.UnmarshalTx(payload.Data)
		require.NoError(t, err)
		require.True(t, proto.Equal(txs[i], tx))
	}
	require.NotNil(t, block.Metadata)
	require.Len(t, block.Metadata.Metadata, 3)
	require.Len(t, block.Metadata.Metadata[2], blockSize)
	for _, status := range block.Metadata.Metadata[2] {
		require.Equal(t, valid, status)
	}
}

func (env *sidecarTestEnv) requireBlock(
	ctx context.Context,
	t *testing.T,
	expectedBlockNumber uint64,
) *common.Block {
	t.Helper()
	checkLastCommittedBlock(ctx, t, env.coordinator, expectedBlockNumber)
	ledger.EnsureAtLeastHeight(t, env.sidecar.GetLedgerService(), expectedBlockNumber+1)

	block, ok := channel.NewReader(ctx, env.committedBlock).Read()
	require.True(t, ok)
	require.NotNil(t, block)
	require.NotNil(t, block.Header)
	require.Equal(t, expectedBlockNumber, block.Header.Number)
	return block
}

func TestConstructStatuses(t *testing.T) {
	t.Parallel()
	statuses := map[string]*protoblocktx.StatusWithHeight{
		"tx1": {
			Code:        protoblocktx.Status_COMMITTED,
			BlockNumber: 1,
			TxNumber:    1,
		},
		"tx2": {
			Code:        protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
			BlockNumber: 1,
			TxNumber:    3,
		},
		"tx3": {
			Code:        protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED,
			BlockNumber: 2,
			TxNumber:    3,
		},
		"tx4": {
			Code:        protoblocktx.Status_COMMITTED,
			BlockNumber: 1,
			TxNumber:    6,
		},
	}
	expectedHeight := map[string]*types.Height{
		"tx1": types.NewHeight(1, 1),
		"tx2": types.NewHeight(1, 3),
		"tx3": types.NewHeight(1, 5),
		"tx4": types.NewHeight(1, 6),
	}

	expectedFinalStatuses := []byte{
		byte(peer.TxValidationCode_VALID),
		byte(protoblocktx.Status_COMMITTED),
		byte(peer.TxValidationCode_VALID),
		byte(protoblocktx.Status_ABORTED_SIGNATURE_INVALID),
		byte(peer.TxValidationCode_VALID),
		byte(protoblocktx.Status_ABORTED_DUPLICATE_TXID),
		byte(protoblocktx.Status_COMMITTED),
	}
	actualFinalStatuses := newValidationCodes(7)
	for _, skippedIdx := range []int{0, 2, 4} {
		actualFinalStatuses[skippedIdx] = byte(peer.TxValidationCode_VALID)
	}
	require.NoError(t, fillStatuses(actualFinalStatuses, statuses, expectedHeight))
	require.Equal(t, expectedFinalStatuses, actualFinalStatuses)
}

func checkLastCommittedBlock(
	ctx context.Context,
	t *testing.T,
	coordinator *mock.Coordinator,
	expectedBlockNumber uint64,
) {
	t.Helper()
	require.Eventually(t, func() bool {
		lastBlock, err := coordinator.GetLastCommittedBlockNumber(ctx, nil)
		if err != nil {
			return false
		}
		return expectedBlockNumber == lastBlock.Number
	}, expectedProcessingTime, 50*time.Millisecond)
}
