package runner

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/broadcastdeliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/sidecarclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	configtempl "github.ibm.com/decentralized-trust-research/scalable-committer/config/templates"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverificationtest "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

type (
	// Cluster represents a test cluster of Coordinator, SigVerifier, VCService and Query processes.
	Cluster struct {
		mockOrderer  *processWithConfig[*configtempl.OrdererConfig]
		sidecar      *processWithConfig[*configtempl.SidecarConfig]
		coordinator  *processWithConfig[*configtempl.CoordinatorConfig]
		queryService *processWithConfig[*configtempl.QueryServiceOrVCServiceConfig]
		sigVerifier  []*processWithConfig[*configtempl.SigVerifierConfig]
		vcService    []*processWithConfig[*configtempl.QueryServiceOrVCServiceConfig]
		loadGen      *processWithConfig[*configtempl.LoadGenConfig]

		dbEnv *vcservice.DatabaseTestEnv

		ordererClient      *broadcastdeliver.EnvelopedStream
		coordinatorClient  protocoordinatorservice.CoordinatorClient
		QueryServiceClient protoqueryservice.QueryServiceClient
		sidecarClient      *sidecarclient.Client

		committedBlock chan *common.Block
		rootDir        string

		nsToCrypto     map[string]*Crypto
		nsToCryptoLock sync.Mutex

		clusterConfig *Config

		channelID        string
		seedForCryptoGen *rand.Rand
	}

	// Crypto holds crypto material for a namespace.
	Crypto struct {
		Namespace  string
		Profile    *workload.Policy
		HashSigner *workload.HashSignerVerifier
		NsSigner   sigverificationtest.NsSigner
		PubKey     []byte
		PubKeyPath string
	}

	// Config represents the configuration of the cluster.
	Config struct {
		NumSigVerifiers     int
		NumVCService        int
		InitializeNamespace []string
		BlockSize           uint64
		BlockTimeout        time.Duration
		LoadGen             bool
	}
)

// NewCluster creates a new test cluster.
func NewCluster(t *testing.T, clusterConfig *Config) *Cluster {
	dir, err := os.Getwd()
	require.NoError(t, err)
	t.Logf("Working dir: %s", dir)
	buildCmd := exec.Command("make", "build")
	buildCmd.Dir = path.Clean(path.Join(dir, "../.."))
	makeRun := run(buildCmd, "make", "")
	select {
	case err = <-makeRun.Wait():
		require.NoError(t, err)
	case <-time.After(3 * time.Minute):
		makeRun.Signal(os.Kill)
		t.Fatalf("Failed to build binaries")
	}

	c := &Cluster{
		sigVerifier: make([]*processWithConfig[*configtempl.SigVerifierConfig], clusterConfig.NumSigVerifiers),
		vcService: make(
			[]*processWithConfig[*configtempl.QueryServiceOrVCServiceConfig],
			clusterConfig.NumSigVerifiers,
		),
		dbEnv:            vcservice.NewDatabaseTestEnv(t),
		rootDir:          t.TempDir(),
		nsToCrypto:       make(map[string]*Crypto),
		clusterConfig:    clusterConfig,
		channelID:        "channel1",
		committedBlock:   make(chan *common.Block, 100),
		seedForCryptoGen: rand.New(rand.NewSource(10)),
	}

	// Start mock ordering service
	ordererEndpoint := makeLocalListenAddress(findAvailablePortRange(t, 1)[0])
	metaCrypto := c.CreateCryptoForNs(t, types.MetaNamespaceID, signature.Ecdsa)
	configBlockPath := configtempl.CreateConfigBlock(t, &configtempl.ConfigBlock{
		ChannelID: c.channelID,
		OrdererEndpoints: []*connection.OrdererEndpoint{
			{MspID: "org", Endpoint: *connection.CreateEndpoint(ordererEndpoint)},
		},
		MetaNamespaceVerificationKey: metaCrypto.PubKey,
	})
	ordererConfig := &configtempl.OrdererConfig{
		ServerEndpoint:  ordererEndpoint,
		BlockSize:       clusterConfig.BlockSize,
		BlockTimeout:    clusterConfig.BlockTimeout,
		ConfigBlockPath: configBlockPath,
	}
	c.mockOrderer = newProcess(t, mockordererCmd, c.rootDir, ordererConfig)

	// Start signature-verifier
	for i := range clusterConfig.NumSigVerifiers {
		c.sigVerifier[i] = newProcess(t, signatureverifierCmd, c.rootDir, &configtempl.SigVerifierConfig{
			CommonEndpoints: newCommonEndpoints(t),
		})
	}

	// Start validator-persister
	for i := range clusterConfig.NumVCService {
		c.vcService[i] = newProcess(t, validatorpersisterCmd, c.rootDir, newQueryServiceOrVCServiceConfig(t, c.dbEnv))
	}

	// Start coordinator
	coordConfig := &configtempl.CoordinatorConfig{
		CommonEndpoints:      newCommonEndpoints(t),
		SigVerifierEndpoints: make([]string, len(c.sigVerifier)),
		VCServiceEndpoints:   make([]string, len(c.vcService)),
	}
	for i, sv := range c.sigVerifier {
		coordConfig.SigVerifierEndpoints[i] = sv.config.ServerEndpoint
	}
	for i, vc := range c.vcService {
		coordConfig.VCServiceEndpoints[i] = vc.config.ServerEndpoint
	}
	c.coordinator = newProcess(t, coordinatorCmd, c.rootDir, coordConfig)

	// Start query-executor
	c.queryService = newProcess(t, queryexecutorCmd, c.rootDir, newQueryServiceOrVCServiceConfig(t, c.dbEnv))

	// Start sidecar. The meta namespace key and the orderer endpoints are passed via the config block.
	sidecarConfig := &configtempl.SidecarConfig{
		CommonEndpoints:     newCommonEndpoints(t),
		CoordinatorEndpoint: c.coordinator.config.ServerEndpoint,
		LedgerPath:          c.rootDir,
		ChannelID:           c.channelID,
		ConfigBlockPath:     configBlockPath,
	}
	c.sidecar = newProcess(t, sidecarCmd, c.rootDir, sidecarConfig)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)

	c.createClients(ctx, t)
	c.ensureLastCommittedBlockNumber(t, 0)

	test.RunServiceForTest(ctx, t, func(ctx context.Context) error {
		return c.sidecarClient.Deliver(ctx, &sidecarclient.DeliverConfig{
			EndBlkNum:   math.MaxUint64,
			OutputBlock: c.committedBlock,
		})
	}, func(ctx context.Context) bool {
		select {
		case <-ctx.Done():
			return false
		case b := <-c.committedBlock:
			require.NotNil(t, b)
			return true
		}
	})

	c.createNamespacesAndCommit(t, clusterConfig.InitializeNamespace)

	// We need to run the load gen after initializing because it will re-initialize.
	if clusterConfig.LoadGen {
		loadgenConfig := &configtempl.LoadGenConfig{
			CommonEndpoints:     newCommonEndpoints(t),
			SidecarEndpoint:     c.sidecar.config.ServerEndpoint,
			CoordinatorEndpoint: c.coordinator.config.ServerEndpoint,
			OrdererEndpoints:    []string{c.mockOrderer.config.ServerEndpoint},
			ChannelID:           c.channelID,
			BlockSize:           clusterConfig.BlockSize,
			Policy: &workload.PolicyProfile{
				NamespacePolicies: make(map[string]*workload.Policy),
			},
		}
		for _, cr := range c.GetAllCrypto() {
			loadgenConfig.Policy.NamespacePolicies[cr.Namespace] = cr.Profile
		}
		c.loadGen = newProcess(t, loadgenCmd, c.rootDir, loadgenConfig)
		return c
	}

	return c
}

// createClients utilize createClientConnection for connection creation
// and responsible for the creation of the clients.
func (c *Cluster) createClients(ctx context.Context, t *testing.T) {
	coordConn := createClientConnection(t, c.coordinator.config.ServerEndpoint)
	c.coordinatorClient = protocoordinatorservice.NewCoordinatorClient(coordConn)

	qsConn := createClientConnection(t, c.queryService.config.ServerEndpoint)
	c.QueryServiceClient = protoqueryservice.NewQueryServiceClient(qsConn)

	ordererSubmitter, err := broadcastdeliver.New(&broadcastdeliver.Config{
		Endpoints: []*connection.OrdererEndpoint{
			{MspID: "org", Endpoint: *connection.CreateEndpoint(c.mockOrderer.config.ServerEndpoint)},
		},
		SignedEnvelopes: false,
		ChannelID:       c.channelID,
		ConsensusType:   broadcastdeliver.Bft,
	})
	require.NoError(t, err)
	c.ordererClient, err = ordererSubmitter.Broadcast(ctx)
	require.NoError(t, err)

	c.sidecarClient, err = sidecarclient.New(&sidecarclient.Config{
		ChannelID: c.channelID,
		Endpoint:  connection.CreateEndpoint(c.sidecar.config.ServerEndpoint),
	})
	require.NoError(t, err)
}

// createNamespacesAndCommit creates namespaces in the committer.
func (c *Cluster) createNamespacesAndCommit(t *testing.T, namespaces []string) {
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
	},
	)
}

// AddSignatures adds signature for each namespace in a given transaction.
func (c *Cluster) AddSignatures(t *testing.T, tx *protoblocktx.Tx) {
	for idx, ns := range tx.Namespaces {
		nsCr := c.GetCryptoForNs(t, ns.NsId)
		sig, err := nsCr.NsSigner.SignNs(tx, idx)
		require.NoError(t, err)
		tx.Signatures = append(tx.Signatures, sig)
	}
}

// SendTransactionsToOrderer creates a block with given transactions and sent it to the committer.
func (c *Cluster) SendTransactionsToOrderer(t *testing.T, txs []*protoblocktx.Tx) {
	for _, tx := range txs {
		_, resp, err := c.ordererClient.SubmitWithEnv(protoutil.MarshalOrPanic(tx))
		require.NoError(t, err)
		require.Equal(t, common.Status_SUCCESS, resp.Status)
	}
}

// CreateCryptoForNs creates the Crypto materials for a namespace using the signature profile.
func (c *Cluster) CreateCryptoForNs(t *testing.T, nsID string, schema signature.Scheme) *Crypto {
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
func (c *Cluster) GetCryptoForNs(t *testing.T, nsID string) *Crypto {
	c.nsToCryptoLock.Lock()
	defer c.nsToCryptoLock.Unlock()

	cr, ok := c.nsToCrypto[nsID]
	require.True(t, ok)
	return cr
}

// GetAllCrypto returns all the Crypto material.
func (c *Cluster) GetAllCrypto() []*Crypto {
	c.nsToCryptoLock.Lock()
	defer c.nsToCryptoLock.Unlock()
	ret := make([]*Crypto, 0, len(c.nsToCrypto))
	for _, cr := range c.nsToCrypto {
		ret = append(ret, cr)
	}
	return ret
}

// createClientConnection creates a service connection using its given server endpoint.
func createClientConnection(t *testing.T, serverEndPoint string) *grpc.ClientConn {
	serviceEndpoint, err := connection.NewEndpoint(serverEndPoint)
	require.NoError(t, err)
	serviceConnection, err := connection.Connect(connection.NewDialConfig(serviceEndpoint))
	require.NoError(t, err)

	return serviceConnection
}

func constructConfigFilePath(rootDir, name, endpoint string) string {
	return path.Join(rootDir, name+"-"+endpoint+"-config.yaml")
}

// ExpectedStatusInBlock holds pairs of expected txID and the corresponding status in a block. The order of statuses
// is expected to be the same as in the committed block.
type ExpectedStatusInBlock struct {
	TxIDs    []string
	Statuses []protoblocktx.Status
}

// ValidateExpectedResultsInCommittedBlock validates the status of transactions in the committed block.
func (c *Cluster) ValidateExpectedResultsInCommittedBlock(t *testing.T, expectedResults *ExpectedStatusInBlock) {
	require.Len(t, expectedResults.Statuses, len(expectedResults.TxIDs))
	blk, ok := <-c.committedBlock
	if !ok {
		return
	}

	expectedStatuses := make([]byte, 0, len(expectedResults.Statuses))
	for _, s := range expectedResults.Statuses {
		expectedStatuses = append(expectedStatuses, byte(s))
	}

	for txNum, txEnv := range blk.Data.Data {
		txBytes, _, err := serialization.UnwrapEnvelope(txEnv)
		require.NoError(t, err)
		tx, err := sidecar.UnmarshalTx(txBytes)
		require.NoError(t, err)
		require.Equal(t, expectedResults.TxIDs[txNum], tx.GetId())
	}

	require.Equal(t, expectedStatuses, blk.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER])

	c.ensureLastCommittedBlockNumber(t, blk.Header.Number)

	nonDuplicateTxIDsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	var nonDupTxIDs []string
	duplicateTxIDsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	for i, tID := range expectedResults.TxIDs {
		s := types.CreateStatusWithHeight(expectedResults.Statuses[i], blk.Header.Number, i)
		if expectedResults.Statuses[i] != protoblocktx.Status_ABORTED_DUPLICATE_TXID {
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

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	test.EnsurePersistedTxStatus(ctx, t, c.coordinatorClient, nonDupTxIDs, nonDuplicateTxIDsStatus)
}

// CountStatus returns the number of transactions with a given tx status.
func (c *Cluster) CountStatus(t *testing.T, status protoblocktx.Status) int {
	return c.dbEnv.CountStatus(t, status)
}

// CountAlternateStatus returns the number of transactions not with a given tx status.
func (c *Cluster) CountAlternateStatus(t *testing.T, status protoblocktx.Status) int {
	return c.dbEnv.CountAlternateStatus(t, status)
}

func (c *Cluster) ensureLastCommittedBlockNumber(t *testing.T, blkNum uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	require.Eventually(t, func() bool {
		lastBlock, err := c.coordinatorClient.GetLastCommittedBlockNumber(ctx, nil)
		if err != nil {
			return false
		}
		return lastBlock.Number == blkNum
	}, 15*time.Second, 250*time.Millisecond)
}

// makeLocalListenAddress returning the endpoint's address together with the port chosen.
func makeLocalListenAddress(port int) string {
	return fmt.Sprintf("localhost:%d", port)
}
