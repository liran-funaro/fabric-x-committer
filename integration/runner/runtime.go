package runner

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/sidecar/sidecarclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/vc"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/vc/dbtest"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/broadcastdeliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature/sigtest"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type (
	// CommitterRuntime represents a test system of Coordinator, SigVerifier, VCService and Query processes.
	CommitterRuntime struct {
		systemConfig config.SystemConfig

		MockOrderer  *ProcessWithConfig
		Sidecar      *ProcessWithConfig
		Coordinator  *ProcessWithConfig
		QueryService *ProcessWithConfig
		Verifier     []*ProcessWithConfig
		VcService    []*ProcessWithConfig

		dbEnv *vc.DatabaseTestEnv

		ordererEndpoints []*connection.OrdererEndpoint

		ordererClient      *broadcastdeliver.EnvelopedStream
		coordinatorClient  protocoordinatorservice.CoordinatorClient
		QueryServiceClient protoqueryservice.QueryServiceClient
		sidecarClient      *sidecarclient.Client

		committedBlock chan *common.Block

		nsToCrypto     map[string]*Crypto
		nsToCryptoLock sync.Mutex

		config *Config

		seedForCryptoGen *rand.Rand

		lastReceivedBlockNumber uint64
	}

	// Crypto holds crypto material for a namespace.
	Crypto struct {
		Namespace  string
		Profile    *workload.Policy
		HashSigner *workload.HashSignerVerifier
		NsSigner   *sigtest.NsSigner
		PubKey     []byte
		PubKeyPath string
	}

	// Config represents the runtime configuration.
	Config struct {
		NumVerifiers        int
		NumVCService        int
		InitializeNamespace []string
		BlockSize           uint64
		BlockTimeout        time.Duration

		// DBCluster configures the cluster to operate in DB cluster mode.
		DBCluster *dbtest.Connection
	}
)

// NewRuntime creates a new test runtime.
func NewRuntime(t *testing.T, conf *Config) *CommitterRuntime {
	t.Helper()

	c := &CommitterRuntime{
		config: conf,
		systemConfig: config.SystemConfig{
			ChannelID:    "channel1",
			BlockSize:    conf.BlockSize,
			BlockTimeout: conf.BlockTimeout,
		},
		nsToCrypto:       make(map[string]*Crypto),
		committedBlock:   make(chan *common.Block, 100),
		seedForCryptoGen: rand.New(rand.NewSource(10)),
	}

	t.Log("Making DB env")
	if conf.DBCluster == nil {
		c.dbEnv = vc.NewDatabaseTestEnv(t)
	} else {
		c.dbEnv = vc.NewDatabaseTestEnvWithCluster(t, conf.DBCluster)
	}

	t.Log("Allocating ports")
	s := &c.systemConfig
	s.Endpoints.Database = c.dbEnv.DBConf.Endpoints

	ports := portAllocator{}
	defer ports.close()
	s.Endpoints.Orderer = ports.allocatePorts(t, 1)
	s.Endpoints.Verifier = ports.allocatePorts(t, conf.NumVerifiers)
	s.Endpoints.VCService = ports.allocatePorts(t, conf.NumVCService)
	s.Endpoints.Query = ports.allocatePorts(t, 1)[0]
	s.Endpoints.Coordinator = ports.allocatePorts(t, 1)[0]
	s.Endpoints.Sidecar = ports.allocatePorts(t, 1)[0]
	s.Endpoints.LoadGen = ports.allocatePorts(t, 1)[0]
	s.DB.Name = c.dbEnv.DBConf.Database
	s.DB.LoadBalance = c.dbEnv.DBConf.LoadBalance
	s.LedgerPath = t.TempDir()

	t.Log("Creating config block")
	c.ordererEndpoints = make([]*connection.OrdererEndpoint, len(s.Endpoints.Orderer))
	for i, endpoint := range s.Endpoints.Orderer {
		c.ordererEndpoints[i] = &connection.OrdererEndpoint{MspID: "org", Endpoint: *endpoint}
	}
	metaCrypto := c.CreateCryptoForNs(t, types.MetaNamespaceID, signature.Ecdsa)
	s.ConfigBlockPath = config.CreateConfigBlock(t, &config.ConfigBlock{
		ChannelID:                    s.ChannelID,
		OrdererEndpoints:             c.ordererEndpoints,
		MetaNamespaceVerificationKey: metaCrypto.PubKey,
	})

	c.MockOrderer = newProcess(t, mockordererCMD, config.TemplateMockOrderer, s)
	for _, e := range s.Endpoints.Verifier {
		c.Verifier = append(c.Verifier, newProcess(t, verifierCMD, config.TemplateVerifier, s.WithEndpoint(e)))
	}
	for _, e := range s.Endpoints.VCService {
		c.VcService = append(c.VcService, newProcess(t, vcCMD, config.TemplateVC, s.WithEndpoint(e)))
	}
	c.Coordinator = newProcess(t, coordinatorCMD, config.TemplateCoordinator, s.WithEndpoint(s.Endpoints.Coordinator))
	c.QueryService = newProcess(t, queryexecutorCMD, config.TemplateQueryService, s.WithEndpoint(s.Endpoints.Query))
	c.Sidecar = newProcess(t, sidecarCMD, config.TemplateSidecar, s.WithEndpoint(s.Endpoints.Sidecar))
	return c
}

// StartSystem runs all the system services.
func (c *CommitterRuntime) StartSystem(t *testing.T) {
	t.Helper()

	t.Log("Running services")
	c.MockOrderer.Restart(t)
	for _, p := range c.Verifier {
		p.Restart(t)
	}
	for _, p := range c.VcService {
		p.Restart(t)
	}
	c.Coordinator.Restart(t)
	c.QueryService.Restart(t)
	c.Sidecar.Restart(t)

	t.Log("Create clients")
	coordConn := createClientConnection(t, c.systemConfig.Endpoints.Coordinator)
	c.coordinatorClient = protocoordinatorservice.NewCoordinatorClient(coordConn)

	qsConn := createClientConnection(t, c.systemConfig.Endpoints.Query)
	c.QueryServiceClient = protoqueryservice.NewQueryServiceClient(qsConn)

	ordererSubmitter, err := broadcastdeliver.New(&broadcastdeliver.Config{
		Connection: broadcastdeliver.ConnectionConfig{
			Endpoints: c.ordererEndpoints,
		},
		ChannelID:     c.systemConfig.ChannelID,
		ConsensusType: broadcastdeliver.Bft,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	t.Cleanup(cancel)
	c.ordererClient, err = ordererSubmitter.Broadcast(ctx)
	require.NoError(t, err)

	c.CreateSidecarDeliverClient(t)

	t.Log("Validate state")
	c.ensureLastCommittedBlockNumber(t, 0)

	t.Log("Running delivery client")
	test.RunServiceForTest(ctx, t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(c.sidecarClient.Deliver(ctx, &sidecarclient.DeliverConfig{
			EndBlkNum:   broadcastdeliver.MaxBlockNum,
			OutputBlock: c.committedBlock,
		}))
	}, func(ctx context.Context) bool {
		select {
		case <-ctx.Done():
			return false
		case b := <-c.committedBlock:
			require.NotNil(t, b)
			return true
		}
	})

	t.Log("Creating namespaces")
	c.createNamespacesAndCommit(t, c.config.InitializeNamespace)
}

// StartLoadGenOrderer applies load on the orderer.
// We need to run the load gen after initializing because it will re-initialize.
func (c *CommitterRuntime) StartLoadGenOrderer(t *testing.T) {
	t.Helper()
	c.startLoadGenWithTemplate(t, config.TemplateLoadGenOrderer)
}

// StartLoadGenCommitter applies load on the sidecar.
// We need to run the load gen after initializing because it will re-initialize.
func (c *CommitterRuntime) StartLoadGenCommitter(t *testing.T) {
	t.Helper()
	// We kill the orderer so the sidecar will connect to the loadgen.
	c.MockOrderer.Stop(t)
	c.startLoadGenWithTemplate(t, config.TemplateLoadGenCommitter)
}

func (c *CommitterRuntime) startLoadGenWithTemplate(t *testing.T, template string) {
	t.Helper()
	s := c.systemConfig
	s.Policy = &workload.PolicyProfile{
		NamespacePolicies: make(map[string]*workload.Policy),
	}
	// We create the crypto profile for the generated namespace to ensure consistency.
	c.CreateCryptoForNs(t, workload.GeneratedNamespaceID, signature.Ecdsa)
	for _, cr := range c.GetAllCrypto() {
		s.Policy.NamespacePolicies[cr.Namespace] = cr.Profile
	}
	newProcess(t, loadgenCMD, template, s.WithEndpoint(s.Endpoints.LoadGen)).Restart(t)
}

// createClientConnection creates a service connection using its given server endpoint.
func createClientConnection(t *testing.T, e *connection.Endpoint) *grpc.ClientConn {
	t.Helper()
	serviceConnection, err := connection.LazyConnect(connection.NewDialConfig(e))
	require.NoError(t, err)
	return serviceConnection
}

// CreateSidecarDeliverClient creates a sidecar deliver client.
func (c *CommitterRuntime) CreateSidecarDeliverClient(t *testing.T) {
	t.Helper()
	var err error
	c.sidecarClient, err = sidecarclient.New(&sidecarclient.Config{
		ChannelID: c.systemConfig.ChannelID,
		Endpoint:  c.systemConfig.Endpoints.Sidecar,
	})
	require.NoError(t, err)
}

// StartSidecarDeliverClient starts a block deliver stream with the sidecar.
func (c *CommitterRuntime) StartSidecarDeliverClient(ctx context.Context, t *testing.T) {
	t.Helper()
	c.committedBlock = make(chan *common.Block, 100)
	test.RunServiceForTest(ctx, t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(c.sidecarClient.Deliver(ctx, &sidecarclient.DeliverConfig{
			StartBlkNum: int64(c.lastReceivedBlockNumber) + 1, //nolint:gosec // uint64 -> int64
			EndBlkNum:   broadcastdeliver.MaxBlockNum,
			OutputBlock: c.committedBlock,
		}))
	}, nil)
}

// createNamespacesAndCommit creates namespaces in the committer.
func (c *CommitterRuntime) createNamespacesAndCommit(t *testing.T, namespaces []string) {
	t.Helper()
	if len(namespaces) == 0 {
		return
	}

	writeToMetaNs := &protoblocktx.TxNamespace{
		NsId:       types.MetaNamespaceID,
		NsVersion:  types.VersionNumber(0).Bytes(),
		ReadWrites: make([]*protoblocktx.ReadWrite, 0, len(namespaces)),
	}

	for _, nsID := range namespaces {
		nsCr := c.CreateCryptoForNs(t, nsID, signature.Ecdsa)
		nsPolicy := &protoblocktx.NamespacePolicy{
			Scheme:    signature.Ecdsa,
			PublicKey: nsCr.PubKey,
		}
		policyBytes, err := proto.Marshal(nsPolicy)
		require.NoError(t, err)

		writeToMetaNs.ReadWrites = append(writeToMetaNs.ReadWrites, &protoblocktx.ReadWrite{
			Key:   []byte(nsID),
			Value: policyBytes,
		})
	}

	txID := uuid.New().String()

	tx := &protoblocktx.Tx{
		Id: txID,
		Namespaces: []*protoblocktx.TxNamespace{
			writeToMetaNs,
		},
	}
	c.AddSignatures(t, tx)
	c.SendTransactionsToOrderer(t, []*protoblocktx.Tx{tx})
	c.ValidateExpectedResultsInCommittedBlock(t, &ExpectedStatusInBlock{
		TxIDs:    []string{txID},
		Statuses: []protoblocktx.Status{protoblocktx.Status_COMMITTED},
	})
}

// AddSignatures adds signature for each namespace in a given transaction.
func (c *CommitterRuntime) AddSignatures(t *testing.T, tx *protoblocktx.Tx) {
	t.Helper()
	for idx, ns := range tx.Namespaces {
		nsCr := c.GetCryptoForNs(t, ns.NsId)
		sig, err := nsCr.NsSigner.SignNs(tx, idx)
		require.NoError(t, err)
		tx.Signatures = append(tx.Signatures, sig)
	}
}

// SendTransactionsToOrderer creates a block with given transactions and sent it to the committer.
func (c *CommitterRuntime) SendTransactionsToOrderer(t *testing.T, txs []*protoblocktx.Tx) {
	t.Helper()
	for _, tx := range txs {
		_, resp, err := c.ordererClient.SubmitWithEnv(tx)
		require.NoError(t, err)
		require.Equal(t, common.Status_SUCCESS, resp.Status)
	}
}

// CreateCryptoForNs creates the Crypto materials for a namespace using the signature profile.
func (c *CommitterRuntime) CreateCryptoForNs(t *testing.T, nsID string, schema signature.Scheme) *Crypto {
	t.Helper()
	policyMsg := &workload.Policy{
		Scheme: schema,
		Seed:   c.seedForCryptoGen.Int63(),
	}
	hashSigner := workload.NewHashSignerVerifier(policyMsg)
	pubKey, signer := hashSigner.GetVerificationKeyAndSigner()
	cr := &Crypto{
		Namespace:  nsID,
		Profile:    policyMsg,
		HashSigner: hashSigner,
		NsSigner:   signer,
		PubKey:     pubKey,
	}

	c.nsToCryptoLock.Lock()
	defer c.nsToCryptoLock.Unlock()
	require.Nil(t, c.nsToCrypto[nsID])
	c.nsToCrypto[nsID] = cr
	return cr
}

// GetCryptoForNs returns the Crypto material a namespace.
func (c *CommitterRuntime) GetCryptoForNs(t *testing.T, nsID string) *Crypto {
	t.Helper()
	c.nsToCryptoLock.Lock()
	defer c.nsToCryptoLock.Unlock()

	cr, ok := c.nsToCrypto[nsID]
	require.True(t, ok)
	return cr
}

// GetAllCrypto returns all the Crypto material.
func (c *CommitterRuntime) GetAllCrypto() []*Crypto {
	c.nsToCryptoLock.Lock()
	defer c.nsToCryptoLock.Unlock()
	ret := make([]*Crypto, 0, len(c.nsToCrypto))
	for _, cr := range c.nsToCrypto {
		ret = append(ret, cr)
	}
	return ret
}

// ExpectedStatusInBlock holds pairs of expected txID and the corresponding status in a block. The order of statuses
// is expected to be the same as in the committed block.
type ExpectedStatusInBlock struct {
	TxIDs    []string
	Statuses []protoblocktx.Status
}

// ValidateExpectedResultsInCommittedBlock validates the status of transactions in the committed block.
func (c *CommitterRuntime) ValidateExpectedResultsInCommittedBlock(t *testing.T, expected *ExpectedStatusInBlock) {
	t.Helper()
	require.Len(t, expected.Statuses, len(expected.TxIDs))
	blk, ok := <-c.committedBlock
	if !ok {
		return
	}
	c.lastReceivedBlockNumber = blk.Header.Number

	expectedStatuses := make([]byte, 0, len(expected.Statuses))
	for _, s := range expected.Statuses {
		expectedStatuses = append(expectedStatuses, byte(s))
	}

	for txNum, txEnv := range blk.Data.Data {
		txBytes, _, err := serialization.UnwrapEnvelope(txEnv)
		require.NoError(t, err)
		tx, err := serialization.UnmarshalTx(txBytes)
		require.NoError(t, err)
		require.Equal(t, expected.TxIDs[txNum], tx.GetId())
	}

	require.Equal(t, expectedStatuses, blk.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER])

	c.ensureLastCommittedBlockNumber(t, blk.Header.Number)

	nonDuplicateTxIDsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	var nonDupTxIDs []string
	duplicateTxIDsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	for i, tID := range expected.TxIDs {
		s := types.CreateStatusWithHeight(expected.Statuses[i], blk.Header.Number, i)
		if expected.Statuses[i] != protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			nonDuplicateTxIDsStatus[tID] = s
			nonDupTxIDs = append(nonDupTxIDs, tID)
			continue
		}
		duplicateTxIDsStatus[tID] = s
	}

	c.dbEnv.StatusExistsForNonDuplicateTxID(t, nonDuplicateTxIDsStatus)
	// For the duplicate txID, neither the status nor the height would match the entry in the
	// transaction status table.
	c.dbEnv.StatusExistsWithDifferentHeightForDuplicateTxID(t, duplicateTxIDsStatus)

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Minute)
	defer cancel()
	test.EnsurePersistedTxStatus(ctx, t, c.coordinatorClient, nonDupTxIDs, nonDuplicateTxIDsStatus)
}

// CountStatus returns the number of transactions with a given tx status.
func (c *CommitterRuntime) CountStatus(t *testing.T, status protoblocktx.Status) int {
	t.Helper()
	return c.dbEnv.CountStatus(t, status)
}

// CountAlternateStatus returns the number of transactions not with a given tx status.
func (c *CommitterRuntime) CountAlternateStatus(t *testing.T, status protoblocktx.Status) int {
	t.Helper()
	return c.dbEnv.CountAlternateStatus(t, status)
}

func (c *CommitterRuntime) ensureLastCommittedBlockNumber(t *testing.T, blkNum uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Minute)
	defer cancel()

	require.Eventually(t, func() bool {
		lastBlock, err := c.coordinatorClient.GetLastCommittedBlockNumber(ctx, nil)
		if err != nil {
			return false
		}
		return lastBlock.Number == blkNum
	}, 15*time.Second, 250*time.Millisecond)
}
