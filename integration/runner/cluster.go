package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sync"
	"testing"
	"text/template"
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
		MockOrdererProcess   *OrdererProcess
		SidecarProcess       *SidecarProcess
		CoordinatorProcess   *CoordinatorProcess
		QueryServiceProcess  *QueryServiceProcess
		SigVerifierProcesses []*SigVerifierProcess
		VCServiceProcesses   []*VCServiceProcess

		OrdererClient      orderer.AtomicBroadcast_BroadcastClient
		CoordinatorClient  protocoordinatorservice.CoordinatorClient
		QueryServiceClient protoqueryservice.QueryServiceClient
		SidecarClient      *deliverclient.Receiver

		EnvelopeCreator broadcastclient.EnvelopeCreator

		CommittedBlock chan *common.Block
		RootDir        string

		PubKey   *sync.Map
		PvtKey   *sync.Map
		TxSigner *sync.Map

		ClusterConfig *Config

		ChannelID string
	}

	// Config represents the configuration of the cluster.
	Config struct {
		NumSigVerifiers     int
		NumVCService        int
		InitializeNamespace []types.NamespaceID
		BlockSize           uint64
		BlockTimeout        time.Duration
	}
)

// NewCluster creates a new test cluster.
func NewCluster(t *testing.T, clusterConfig *Config) *Cluster {
	tempDir, err := os.MkdirTemp("", "cluster")
	require.NoError(t, err)

	c := &Cluster{
		SigVerifierProcesses: make([]*SigVerifierProcess, 0, clusterConfig.NumSigVerifiers),
		VCServiceProcesses:   make([]*VCServiceProcess, 0, clusterConfig.NumSigVerifiers),
		RootDir:              tempDir,
		PubKey:               &sync.Map{},
		PvtKey:               &sync.Map{},
		TxSigner:             &sync.Map{},
		ClusterConfig:        clusterConfig,
		ChannelID:            "channel1",
		CommittedBlock:       make(chan *common.Block, 100),
	}

	for i := 0; i < clusterConfig.NumSigVerifiers; i++ {
		c.SigVerifierProcesses = append(c.SigVerifierProcesses, NewSigVerifierProcess(t, c.RootDir))
	}

	dbEnv := vcservice.NewDatabaseTestEnv(t)
	require.NoError(t, vcservice.InitDatabase(dbEnv.DBConf))
	for i := 0; i < clusterConfig.NumVCService; i++ {
		c.VCServiceProcesses = append(c.VCServiceProcesses, NewVCServiceProcess(t, c.RootDir, dbEnv))
	}

	c.CoordinatorProcess = NewCoordinatorProcess(t, c.SigVerifierProcesses, c.VCServiceProcesses, c.RootDir)

	c.QueryServiceProcess = NewQueryServiceProcess(t, c.RootDir, dbEnv)

	c.MockOrdererProcess = NewOrdererProcess(t, c.RootDir, clusterConfig.BlockSize, clusterConfig.BlockTimeout)

	c.SidecarProcess = NewSidecarProcess(t, c.MockOrdererProcess, c.CoordinatorProcess, c.RootDir, c.ChannelID)

	c.createClients(t)
	c.setMetaNamespaceVerificationKey(t)
	c.ensureLastCommittedBlockNumber(t, 0)
	go func() {
		require.True(t, connection.IsStreamEnd(c.SidecarClient.Run(context.TODO())))
	}()
	<-c.CommittedBlock
	c.createNamespacesAndCommit(t, clusterConfig.InitializeNamespace)

	return c
}

// createClients utilize createClientConnection for connection creation
// and responsible for the creation of the clients.
func (c *Cluster) createClients(t *testing.T) {
	coordConn := createClientConnection(t, c.CoordinatorProcess.Config.ServerEndpoint)
	c.CoordinatorClient = protocoordinatorservice.NewCoordinatorClient(coordConn)

	qsConn := createClientConnection(t, c.QueryServiceProcess.Config.ServerEndpoint)
	c.QueryServiceClient = protoqueryservice.NewQueryServiceClient(qsConn)

	ordererClient, envelopeCreator, err := broadcastclient.New(broadcastclient.Config{
		Broadcast:       []*connection.Endpoint{connection.CreateEndpoint(c.MockOrdererProcess.Config.ServerEndpoint)},
		SignedEnvelopes: false,
		ChannelID:       c.ChannelID,
		Type:            utils.Raft,
		Parallelism:     1,
	})
	require.NoError(t, err)
	c.OrdererClient = ordererClient[0]
	c.EnvelopeCreator = envelopeCreator

	c.SidecarClient, err = deliverclient.New(&deliverclient.Config{
		ChannelID: c.ChannelID,
		Endpoint:  *connection.CreateEndpoint(c.SidecarProcess.Config.ServerEndpoint),
		Reconnect: -1,
	}, deliverclient.Ledger, c.CommittedBlock)
	require.NoError(t, err)
}

func (c *Cluster) setMetaNamespaceVerificationKey(t *testing.T) {
	c.CreateCryptoForNs(t, types.MetaNamespaceID, &signature.Profile{
		Scheme: signature.Ecdsa,
	})
	metaPubKey, ok := c.PubKey.Load(types.MetaNamespaceID)
	require.True(t, ok)

	_, err := c.CoordinatorClient.SetMetaNamespaceVerificationKey(
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
		env, _, err := c.EnvelopeCreator.CreateEnvelope(protoutil.MarshalOrPanic(tx))
		require.NoError(t, err)
		require.NoError(t, c.OrdererClient.Send(env))
		resp, err := c.OrdererClient.Recv()
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
	c.PubKey.Store(nsID, pubKey)
	c.PvtKey.Store(nsID, pvtKey)
	c.TxSigner.Store(nsID, txSigner)
}

// GetPublicKey returns the public key for a namespace.
func (c *Cluster) GetPublicKey(t *testing.T, nsID types.NamespaceID) []byte {
	pubKey, ok := c.PubKey.Load(nsID)
	if !ok {
		return nil
	}
	k, ok := pubKey.([]byte)
	require.True(t, ok)
	return k
}

// GetTxSigner returns the transaction signer for a namespace.
func (c *Cluster) getTxSigner(t *testing.T, nsID types.NamespaceID) sigverificationtest.NsSigner {
	tSigner, ok := c.TxSigner.Load(nsID)
	if !ok {
		return nil
	}
	k, ok := tSigner.(sigverificationtest.NsSigner)
	require.True(t, ok)
	return k
}

// Stop stops the cluster.
func (c *Cluster) Stop(t *testing.T) {
	process := make([]ifrit.Process, 0, len(c.SigVerifierProcesses)+len(c.VCServiceProcesses)+4)

	for _, sigVerifier := range c.SigVerifierProcesses {
		process = append(process, sigVerifier.Process)
	}
	for _, vcser := range c.VCServiceProcesses {
		process = append(process, vcser.Process)
	}
	process = append(process, c.CoordinatorProcess.Process)
	process = append(process, c.SidecarProcess.Process)
	process = append(process, c.QueryServiceProcess.Process)
	process = append(process, c.MockOrdererProcess.Process)

	var wg sync.WaitGroup
	wg.Add(len(process))
	for _, p := range process {
		go killAndWait(&wg, p)
	}
	wg.Wait()

	require.NoError(t, os.RemoveAll(c.RootDir))
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
	gomega.Eventually(process.Ready(), 20*time.Second, 1*time.Second).Should(gomega.BeClosed())
	return process
}

func constructConfigFilePath(rootDir, name, endpoint string) string {
	return path.Join(rootDir, name+"-"+endpoint+"-config.yaml")
}

func createConfigFile(t *testing.T, config any, templateFilePath, outputFilePath string) {
	tmpl, err := template.ParseFiles(templateFilePath)
	require.NoError(t, err)

	var renderedConfig bytes.Buffer
	err = tmpl.Execute(&renderedConfig, config)
	require.NoError(t, err)

	outputFile, err := os.Create(outputFilePath)
	require.NoError(t, err)
	defer func() {
		_ = outputFile.Close()
	}()

	_, err = outputFile.Write(renderedConfig.Bytes())
	require.NoError(t, err)
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
	blk, ok := <-c.CommittedBlock
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

func (c *Cluster) ensureLastCommittedBlockNumber(t *testing.T, blkNum uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	require.Eventually(t, func() bool {
		lastBlock, err := c.CoordinatorClient.GetLastCommittedBlockNumber(ctx, nil)
		if err != nil {
			return false
		}
		return lastBlock.Number == blkNum
	}, 10*time.Second, 250*time.Millisecond)
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
