package cluster

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
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverificationtest "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"google.golang.org/grpc"
)

// Cluster represents a test cluster of Coordinator, SigVerifier, VCService and Query processes.
type Cluster struct {
	CoordinatorProcess   *CoordinatorProcess
	QueryServiceProcess  *QueryServiceProcess
	SigVerifierProcesses []*SigVerifierProcess
	VCServiceProcesses   []*VCServiceProcess
	RootDir              string
	CoordinatorClient    protocoordinatorservice.CoordinatorClient
	QueryServiceClient   protoqueryservice.QueryServiceClient

	PubKey   *sync.Map
	PvtKey   *sync.Map
	TxSigner *sync.Map

	ClusterConfig *Config

	NextBlockNumber uint64
	Stream          protocoordinatorservice.Coordinator_BlockProcessingClient
}

// Config represents the configuration of the cluster.
type Config struct {
	NumSigVerifiers     int
	NumVCService        int
	InitializeNamespace []types.NamespaceID
}

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
	}

	for i := 0; i < clusterConfig.NumSigVerifiers; i++ {
		ports := findAvailablePortRange(t, numPortsPerSigVerifier)
		c.SigVerifierProcesses = append(c.SigVerifierProcesses, NewSigVerifierProcess(t, ports, tempDir))
	}

	dbEnv := vcservice.NewDatabaseTestEnv(t)
	require.NoError(t, vcservice.InitDatabase(dbEnv.DBConf, nil))
	for i := 0; i < clusterConfig.NumVCService; i++ {
		ports := findAvailablePortRange(t, numPortsPerVCService)
		c.VCServiceProcesses = append(c.VCServiceProcesses, NewVCServiceProcess(t, ports, tempDir, dbEnv))
	}

	ports := findAvailablePortRange(t, numPortsForCoordinator)
	c.CoordinatorProcess = NewCoordinatorProcess(t, ports, c.SigVerifierProcesses, c.VCServiceProcesses, c.RootDir)

	ports = findAvailablePortRange(t, numPortsPerQueryService)
	c.QueryServiceProcess = NewQueryServiceProcess(t, ports, c.RootDir, dbEnv)

	c.createClients(t)
	c.setMetaNamespaceVerificationKey(t)
	c.createBlockProcessingStream(t)
	c.CreateNamespacesAndCommit(t, clusterConfig.InitializeNamespace)

	return c
}

// createClients utilize createClientConnection for connection creation
// and responsible for the creation of the clients.
func (c *Cluster) createClients(t *testing.T) {
	serviceConnection := createClientConnection(t, c.CoordinatorProcess.Config.ServerEndpoint)
	c.CoordinatorClient = protocoordinatorservice.NewCoordinatorClient(serviceConnection)

	serviceConnection = createClientConnection(t, c.QueryServiceProcess.Config.ServerEndpoint)
	c.QueryServiceClient = protoqueryservice.NewQueryServiceClient(serviceConnection)
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

func (c *Cluster) createBlockProcessingStream(t *testing.T) {
	blockStream, err := c.CoordinatorClient.BlockProcessing(context.Background())
	require.NoError(t, err)
	c.Stream = blockStream // nolint:ireturn
}

// CreateNamespacesAndCommit creates namespaces in the committer.
func (c *Cluster) CreateNamespacesAndCommit(t *testing.T, namespaces []types.NamespaceID) {
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
	c.SendTransactions(t, []*protoblocktx.Tx{tx})
	c.ValidateStatus(t, map[string]protoblocktx.Status{
		txID: protoblocktx.Status_COMMITTED,
	})
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

// SendTransactions creates a block with given transactions and sent it to the committer.
func (c *Cluster) SendTransactions(t *testing.T, txs []*protoblocktx.Tx) {
	blk := &protoblocktx.Block{
		Number: c.NextBlockNumber,
		Txs:    txs,
	}

	require.NoError(t, c.Stream.Send(blk))
	c.NextBlockNumber++
}

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
	var wg sync.WaitGroup
	totalNumberOfServiceProcesses := len(c.SigVerifierProcesses) + len(c.VCServiceProcesses) + 2
	wg.Add(totalNumberOfServiceProcesses)

	for _, sigVerifierProcess := range c.SigVerifierProcesses {
		go sigVerifierProcess.killAndWait(&wg)
	}

	for _, vcserviceProcess := range c.VCServiceProcesses {
		go vcserviceProcess.killAndWait(&wg)
	}

	go c.CoordinatorProcess.killAndWait(&wg)
	go c.QueryServiceProcess.killAndWait(&wg)

	wg.Wait()

	require.NoError(t, os.RemoveAll(c.RootDir))
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

// ValidateStatus validates the status of transactions.
func (c *Cluster) ValidateStatus(
	t *testing.T,
	expectedTxStatus map[string]protoblocktx.Status,
) {
	processed := 0
	for {
		status, err := c.Stream.Recv()
		require.NoError(t, err)

		for _, txStatus := range status.TxsValidationStatus {
			require.Equal(t, expectedTxStatus[txStatus.TxId], txStatus.Status)
		}

		processed += len(status.TxsValidationStatus)
		if processed == len(expectedTxStatus) {
			break
		}
	}
}

// makeLocalListenAddress returning the endpoint's address together with the port chosen.
func makeLocalListenAddress(port int) string {
	return fmt.Sprintf("localhost:%d", port)
}
