package cluster

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path"
	"sync"
	"testing"
	"text/template"
	"time"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

// Cluster represents a test cluster of Coordinator, SigVerifier and VCService processes.
type Cluster struct {
	CoordinatorProcess   *CoordinatorProcess
	SigVerifierProcesses []*SigVerifierProcess
	VCServiceProcesses   []*VCServiceProcess
	RootDir              string
	CoordinatorClient    protocoordinatorservice.CoordinatorClient

	PubKey   *sync.Map
	PvtKey   *sync.Map
	TxSigner *sync.Map

	ClusterConfig *Config
}

// Config represents the configuration of the cluster.
type Config struct {
	NumSigVerifiers int
	NumVCService    int
}

// NewCluster creates a new test cluster.
func NewCluster(t *testing.T, clusterConfig *Config) *Cluster {
	tempDir, err := os.MkdirTemp("", "cluster")
	require.NoError(t, err)

	c := &Cluster{
		RootDir:  tempDir,
		PubKey:   &sync.Map{},
		PvtKey:   &sync.Map{},
		TxSigner: &sync.Map{},
	}

	var ports []int
	for i := 0; i < clusterConfig.NumSigVerifiers; i++ {
		ports, err = FindAvailablePortRange(numPortsPerSigVerifier)
		require.NoError(t, err)
		c.SigVerifierProcesses = append(
			c.SigVerifierProcesses,
			NewSigVerifierProcess(t, ports, tempDir),
		)
	}

	dbEnv := vcservice.NewDatabaseTestEnv(t)
	require.NoError(t, vcservice.InitDatabase(dbEnv.DBConf, nil))
	for i := 0; i < clusterConfig.NumVCService; i++ {
		ports, err = FindAvailablePortRange(numPortsPerVCService)
		require.NoError(t, err)
		c.VCServiceProcesses = append(
			c.VCServiceProcesses,
			NewVCServiceProcess(t, ports, tempDir, dbEnv),
		)
	}

	ports, err = FindAvailablePortRange(numPortsForCoordinator)
	require.NoError(t, err)
	c.CoordinatorProcess = NewCoordinatorProcess(
		t,
		ports,
		c.SigVerifierProcesses,
		c.VCServiceProcesses,
		c.RootDir,
	)

	c.CreateCoordinatorClient(t)

	c.CreateCryptoForNs(t, types.MetaNamespaceID, &signature.Profile{
		Scheme: signature.Ecdsa,
	})
	metaPubKey, ok := c.PubKey.Load(types.MetaNamespaceID)
	require.True(t, ok)

	_, err = c.CoordinatorClient.SetMetaNamespaceVerificationKey(
		context.Background(),
		&protosigverifierservice.Key{
			NsId:            uint32(types.MetaNamespaceID),
			NsVersion:       types.VersionNumber(0).Bytes(),
			SerializedBytes: metaPubKey.([]byte),
			Scheme:          signature.Ecdsa,
		},
	)
	require.NoError(t, err)

	c.ClusterConfig = clusterConfig

	return c
}

// CreateCoordinatorClient creates a client for the coordinator.
func (c *Cluster) CreateCoordinatorClient(t *testing.T) {
	coordEndpoint, err := connection.NewEndpoint(c.CoordinatorProcess.Config.ServerEndpoint)
	require.NoError(t, err)
	coordDialConf := connection.NewDialConfig(*coordEndpoint)
	coordConn, err := connection.Connect(coordDialConf)
	require.NoError(t, err)
	coordClient := protocoordinatorservice.NewCoordinatorClient(coordConn) //nolint:ireturn
	c.CoordinatorClient = coordClient
}

// GetBlockProcessingStream returns a block processing stream from the coordinator.
func (c *Cluster) GetBlockProcessingStream( //nolint:ireturn
	t *testing.T,
) protocoordinatorservice.Coordinator_BlockProcessingClient {
	blockStream, err := c.CoordinatorClient.BlockProcessing(context.Background())
	require.NoError(t, err)
	return blockStream
}

// CreateCryptoForNs creates crypto for a namespace.
func (c *Cluster) CreateCryptoForNs(
	t *testing.T,
	nsID types.NamespaceID,
	sigProfile *signature.Profile,
) {
	factory := sigverification_test.GetSignatureFactory(sigProfile.Scheme)
	pvtKey, pubKey := factory.NewKeys()
	txSigner, err := factory.NewSigner(pvtKey)
	require.NoError(t, err)
	c.PubKey.Store(nsID, pubKey)
	c.PvtKey.Store(nsID, pvtKey)
	c.TxSigner.Store(nsID, txSigner)
}

// GetPublicKey returns the public key for a namespace.
func (c *Cluster) GetPublicKey(nsID types.NamespaceID) []byte {
	pubKey, ok := c.PubKey.Load(nsID)
	if !ok {
		return nil
	}
	k, _ := pubKey.([]byte)
	return k
}

// GetPrivateKey returns the private key for a namespace.
func (c *Cluster) GetPrivateKey(nsID types.NamespaceID) []byte {
	pvtKey, ok := c.PvtKey.Load(nsID)
	if !ok {
		return nil
	}
	k, _ := pvtKey.([]byte)
	return k
}

// GetTxSigner returns the transaction signer for a namespace.
func (c *Cluster) GetTxSigner(nsID types.NamespaceID) sigverification_test.NsSigner {
	tSigner, ok := c.TxSigner.Load(nsID)
	if !ok {
		return nil
	}
	k, _ := tSigner.(sigverification_test.NsSigner)
	return k
}

// Stop stops the cluster.
func (c *Cluster) Stop() {
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for _, sigVerifierProcess := range c.SigVerifierProcesses {
			if sigVerifierProcess != nil {
				sigVerifierProcess.kill()
				<-sigVerifierProcess.Process.Wait()
			}
		}
	}()

	go func() {
		defer wg.Done()
		for _, vcserviceProcess := range c.VCServiceProcesses {
			if vcserviceProcess != nil {
				vcserviceProcess.kill()
				<-vcserviceProcess.Process.Wait()
			}
		}
	}()

	go func() {
		defer wg.Done()
		if c.CoordinatorProcess != nil {
			c.CoordinatorProcess.kill()
			<-c.CoordinatorProcess.Process.Wait()
		}
	}()

	wg.Wait()

	os.RemoveAll(c.RootDir)
}

func run(cmd *exec.Cmd, name string, startCheck string) ifrit.Process {
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
	Eventually(process.Ready(), 20*time.Second, 1*time.Second).Should(BeClosed())
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
	defer outputFile.Close()

	_, err = outputFile.Write(renderedConfig.Bytes())
	require.NoError(t, err)
}

// ValidateStatus validates the status of transactions.
func ValidateStatus(
	t *testing.T,
	expectedTxStatus map[string]protoblocktx.Status,
	blockStream protocoordinatorservice.Coordinator_BlockProcessingClient,
) {
	processed := 0
	for {
		status, err := blockStream.Recv()
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
