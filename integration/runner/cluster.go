package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	configtempl "github.ibm.com/decentralized-trust-research/scalable-committer/config/templates"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/broadcastclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverificationtest "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
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

		ordererClient      orderer.AtomicBroadcast_BroadcastClient
		coordinatorClient  protocoordinatorservice.CoordinatorClient
		QueryServiceClient protoqueryservice.QueryServiceClient
		sidecarClient      *deliverclient.Receiver

		envelopeCreator broadcastclient.EnvelopeCreator

		committedBlock chan *common.Block
		rootDir        string

		pubKey   *sync.Map
		pvtKey   *sync.Map
		txSigner *sync.Map

		clusterConfig *Config

		channelID string
	}

	// Config represents the configuration of the cluster.
	Config struct {
		NumSigVerifiers     int
		NumVCService        int
		InitializeNamespace []types.NamespaceID
		BlockSize           uint64
		BlockTimeout        time.Duration
		LoadGen             bool
	}
)

// NewCluster creates a new test cluster.
func NewCluster(t *testing.T, clusterConfig *Config) *Cluster {
	tempDir, err := os.MkdirTemp("", "cluster")
	require.NoError(t, err)

	c := &Cluster{
		sigVerifier: make([]*processWithConfig[*configtempl.SigVerifierConfig], clusterConfig.NumSigVerifiers),
		vcService: make(
			[]*processWithConfig[*configtempl.QueryServiceOrVCServiceConfig],
			clusterConfig.NumSigVerifiers,
		),
		dbEnv:          vcservice.NewDatabaseTestEnv(t),
		rootDir:        tempDir,
		pubKey:         &sync.Map{},
		pvtKey:         &sync.Map{},
		txSigner:       &sync.Map{},
		clusterConfig:  clusterConfig,
		channelID:      "channel1",
		committedBlock: make(chan *common.Block, 100),
	}

	// Start mock ordering service
	ordererConfig := &configtempl.OrdererConfig{
		ServerEndpoint: makeLocalListenAddress(findAvailablePortRange(t, 1)[0]),
		BlockSize:      clusterConfig.BlockSize,
		BlockTimeout:   clusterConfig.BlockTimeout,
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

	// Start sidecar
	sidecarConfig := &configtempl.SidecarConfig{
		CommonEndpoints:     newCommonEndpoints(t),
		OrdererEndpoint:     c.mockOrderer.config.ServerEndpoint,
		CoordinatorEndpoint: c.coordinator.config.ServerEndpoint,
		LedgerPath:          c.rootDir,
		ChannelID:           c.channelID,
	}
	c.sidecar = newProcess(t, sidecarCmd, c.rootDir, sidecarConfig)

	if clusterConfig.LoadGen {
		loadgenConfig := &configtempl.LoadGenConfig{
			CommonEndpoints:     newCommonEndpoints(t),
			SidecarEndpoint:     c.sidecar.config.ServerEndpoint,
			CoordinatorEndpoint: c.coordinator.config.ServerEndpoint,
			OrdererEndpoints:    []string{c.mockOrderer.config.ServerEndpoint},
			ChannelID:           c.channelID,
			BlockSize:           clusterConfig.BlockSize,
		}
		c.loadGen = newProcess(t, loadgenCmd, c.rootDir, loadgenConfig)
		return c
	}

	c.createClients(t)
	c.setMetaNamespaceVerificationKey(t)
	c.ensureLastCommittedBlockNumber(t, 0)
	go func() {
		require.True(t, connection.IsStreamEnd(c.sidecarClient.Run(context.TODO())))
	}()
	<-c.committedBlock
	c.createNamespacesAndCommit(t, clusterConfig.InitializeNamespace)

	return c
}

// createClients utilize createClientConnection for connection creation
// and responsible for the creation of the clients.
func (c *Cluster) createClients(t *testing.T) {
	coordConn := createClientConnection(t, c.coordinator.config.ServerEndpoint)
	c.coordinatorClient = protocoordinatorservice.NewCoordinatorClient(coordConn)

	qsConn := createClientConnection(t, c.queryService.config.ServerEndpoint)
	c.QueryServiceClient = protoqueryservice.NewQueryServiceClient(qsConn)

	ordererClient, envelopeCreator, err := broadcastclient.New(broadcastclient.Config{
		Broadcast:       []*connection.Endpoint{connection.CreateEndpoint(c.mockOrderer.config.ServerEndpoint)},
		SignedEnvelopes: false,
		ChannelID:       c.channelID,
		Type:            utils.Raft,
		Parallelism:     1,
	})
	require.NoError(t, err)
	c.ordererClient = ordererClient[0]
	c.envelopeCreator = envelopeCreator

	c.sidecarClient, err = deliverclient.New(&deliverclient.Config{
		ChannelID: c.channelID,
		Endpoint:  *connection.CreateEndpoint(c.sidecar.config.ServerEndpoint),
		Reconnect: -1,
	}, deliverclient.Ledger, c.committedBlock)
	require.NoError(t, err)
}

func (c *Cluster) setMetaNamespaceVerificationKey(t *testing.T) {
	c.CreateCryptoForNs(t, types.MetaNamespaceID, &signature.Profile{
		Scheme: signature.Ecdsa,
	})
	metaPubKey, ok := c.pubKey.Load(types.MetaNamespaceID)
	require.True(t, ok)

	_, err := c.coordinatorClient.SetMetaNamespaceVerificationKey(
		context.Background(),
		&protosigverifierservice.Key{
			NsId:            uint32(types.MetaNamespaceID),
			NsVersion:       types.VersionNumber(0).Bytes(),
			SerializedBytes: metaPubKey.([]byte),
			Scheme:          signature.Ecdsa,
		},
	)
	require.NoError(t, err)
}

// createNamespacesAndCommit creates namespaces in the committer.
func (c *Cluster) createNamespacesAndCommit(t *testing.T, namespaces []types.NamespaceID) {
	if len(namespaces) == 0 {
		return
	}

	writeToMetaNs := &protoblocktx.TxNamespace{
		NsId:       uint32(types.MetaNamespaceID),
		NsVersion:  types.VersionNumber(0).Bytes(),
		ReadWrites: make([]*protoblocktx.ReadWrite, 0, len(namespaces)),
	}

	for _, nsID := range namespaces {
		c.CreateCryptoForNs(t, nsID, &signature.Profile{Scheme: signature.Ecdsa})

		nsPolicy := &protoblocktx.NamespacePolicy{
			Scheme:    signature.Ecdsa,
			PublicKey: c.GetPublicKey(t, nsID),
		}
		policyBytes, err := proto.Marshal(nsPolicy)
		require.NoError(t, err)

		writeToMetaNs.ReadWrites = append(writeToMetaNs.ReadWrites, &protoblocktx.ReadWrite{
			Key:   nsID.Bytes(),
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
		tSigner := c.getTxSigner(t, types.NamespaceID(ns.NsId))
		sig, err := tSigner.SignNs(tx, idx)
		require.NoError(t, err)
		tx.Signatures = append(tx.Signatures, sig)
	}
}

// SendTransactionsToOrderer creates a block with given transactions and sent it to the committer.
func (c *Cluster) SendTransactionsToOrderer(t *testing.T, txs []*protoblocktx.Tx) {
	for _, tx := range txs {
		env, _, err := c.envelopeCreator.CreateEnvelope(protoutil.MarshalOrPanic(tx))
		require.NoError(t, err)
		require.NoError(t, c.ordererClient.Send(env))
		resp, err := c.ordererClient.Recv()
		require.NoError(t, err)
		require.Equal(t, common.Status_SUCCESS, resp.Status)
	}
}

// CreateCryptoForNs creates required crypto materials for a given namespace using the signature profile.
func (c *Cluster) CreateCryptoForNs(
	t *testing.T,
	nsID types.NamespaceID,
	sigProfile *signature.Profile,
) {
	factory := sigverificationtest.GetSignatureFactory(sigProfile.Scheme)
	pvtKey, pubKey := factory.NewKeys()
	txSigner, err := factory.NewSigner(pvtKey)
	require.NoError(t, err)
	c.pubKey.Store(nsID, pubKey)
	c.pvtKey.Store(nsID, pvtKey)
	c.txSigner.Store(nsID, txSigner)
}

// GetPublicKey returns the public key for a namespace.
func (c *Cluster) GetPublicKey(t *testing.T, nsID types.NamespaceID) []byte {
	pubKey, ok := c.pubKey.Load(nsID)
	if !ok {
		return nil
	}
	k, ok := pubKey.([]byte)
	require.True(t, ok)
	return k
}

// GetTxSigner returns the transaction signer for a namespace.
func (c *Cluster) getTxSigner(t *testing.T, nsID types.NamespaceID) sigverificationtest.NsSigner {
	tSigner, ok := c.txSigner.Load(nsID)
	if !ok {
		return nil
	}
	k, ok := tSigner.(sigverificationtest.NsSigner)
	require.True(t, ok)
	return k
}

// Stop stops the cluster.
func (c *Cluster) Stop(t *testing.T) {
	process := make([]ifrit.Process, 0, len(c.sigVerifier)+len(c.vcService)+4)

	for _, sigVerifier := range c.sigVerifier {
		process = append(process, sigVerifier.process)
	}
	for _, vcser := range c.vcService {
		process = append(process, vcser.process)
	}
	process = append(process, c.coordinator.process)
	process = append(process, c.sidecar.process)
	process = append(process, c.queryService.process)
	process = append(process, c.mockOrderer.process)

	var wg sync.WaitGroup
	wg.Add(len(process))
	for _, p := range process {
		go killAndWait(&wg, p)
	}
	wg.Wait()

	require.NoError(t, os.RemoveAll(c.rootDir))
}

// createClientConnection creates a service connection using its given server endpoint.
func createClientConnection(t *testing.T, serverEndPoint string) *grpc.ClientConn {
	serviceEndpoint, err := connection.NewEndpoint(serverEndPoint)
	require.NoError(t, err)
	serviceDialConfig := connection.NewDialConfig(*serviceEndpoint)
	serviceConnection, err := connection.Connect(serviceDialConfig)
	require.NoError(t, err)

	return serviceConnection
}

func run(cmd *exec.Cmd, name, startCheck string) ifrit.Process { //nolint:ireturn
	p := ginkgomon.New(ginkgomon.Config{
		Command:           cmd,
		Name:              name,
		AnsiColorCode:     "",
		StartCheck:        startCheck,
		StartCheckTimeout: 0,
		Cleanup: func() {
		},
	})
	process := ifrit.Invoke(p)
	gomega.Eventually(process.Ready(), 3*time.Minute, 1*time.Second).Should(gomega.BeClosed())
	return process
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

func start(cmd, configFilePath, name string) ifrit.Process { //nolint:ireturn
	c := exec.Command(cmd, "start", "--configs", configFilePath)
	return run(c, name, "Serving")
}

func killAndWait(wg *sync.WaitGroup, p ifrit.Process) {
	defer wg.Done()
	if p == nil {
		return
	}
	p.Signal(os.Kill)
	<-p.Wait()
}

// makeLocalListenAddress returning the endpoint's address together with the port chosen.
func makeLocalListenAddress(port int) string {
	return fmt.Sprintf("localhost:%d", port)
}
