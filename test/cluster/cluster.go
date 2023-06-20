package cluster

import (
	"bytes"
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"sync"
	"text/template"

	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.ibm.com/distributed-trust-research/scalable-committer/protos/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/protos/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type Cluster struct {
	CoordinatorProcess   *CoordinatorProcess
	SigVerifierProcesses []*SigVerifierProcess
	ShardServerProcesses []*ShardServerProcess
	RootDir              string
	PubKey               []byte
	PvtKey               []byte
	CoordinatorClient    coordinatorservice.CoordinatorClient
	TxSigner             sigverification_test.TxSigner

	ClusterConfig *ClusterConfig
}

type ClusterConfig struct {
	NumSigVerifiers   int
	NumShardServers   int
	NumShardPerServer int
	SigProfile        signature.Profile
}

func NewCluster(clusterConfig *ClusterConfig) *Cluster {
	coordStartPort := CoordinatorBasePort.StartPort()
	sigVerifierStartPort := SigVerifierBasePort.StartPort()
	shardServerStartPort := ShardServerBasePort.StartPort()

	tempDir, err := ioutil.TempDir("", "cluster")
	Expect(err).NotTo(HaveOccurred())

	c := &Cluster{
		RootDir: tempDir,
	}

	for i := 0; i < clusterConfig.NumSigVerifiers; i++ {
		c.SigVerifierProcesses = append(c.SigVerifierProcesses, NewSigVerifierProcess(sigVerifierStartPort, tempDir))
		sigVerifierStartPort += portsPerNode
	}

	for i := 0; i < clusterConfig.NumShardServers; i++ {
		c.ShardServerProcesses = append(c.ShardServerProcesses, NewShardServerProcess(shardServerStartPort, tempDir))
		shardServerStartPort += portsPerNode
	}

	c.CoordinatorProcess = NewCoordinatorProcess(coordStartPort, c.SigVerifierProcesses, c.ShardServerProcesses, clusterConfig.NumShardPerServer, c.RootDir)

	pvtKey, pubKey, err := sigverification_test.ReadOrGenerateKeys(clusterConfig.SigProfile)
	Expect(err).NotTo(HaveOccurred())

	c.PubKey = pubKey
	c.PvtKey = pvtKey

	c.CreateCoordinatorClient()

	c.ClusterConfig = clusterConfig

	txSigner, err := sigverification_test.GetSignatureFactory(c.ClusterConfig.SigProfile.Scheme).NewSigner(c.PvtKey)
	Expect(err).NotTo(HaveOccurred())
	c.TxSigner = txSigner

	return c
}

func (c *Cluster) CreateCoordinatorClient() {
	coordEndpoint, err := connection.NewEndpoint(c.CoordinatorProcess.Config.ServerEndpoint)
	Expect(err).NotTo(HaveOccurred())
	coordDialConf := connection.NewDialConfig(*coordEndpoint)
	coordConn, err := connection.Connect(coordDialConf)
	Expect(err).NotTo(HaveOccurred())

	coordClient := coordinatorservice.NewCoordinatorClient(coordConn)
	_, err = coordClient.SetVerificationKey(
		context.Background(),
		&sigverification.Key{
			SerializedBytes: c.PubKey,
		},
	)
	Expect(err).NotTo(HaveOccurred())
	c.CoordinatorClient = coordClient
}

func (c *Cluster) GetBlockProcessingStream() coordinatorservice.Coordinator_BlockProcessingClient {
	blockStream, err := c.CoordinatorClient.BlockProcessing(context.Background())
	Expect(err).NotTo(HaveOccurred())
	return blockStream
}

func (c *Cluster) Stop() {
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for _, sigVerifierProcess := range c.SigVerifierProcesses {
			if sigVerifierProcess != nil {
				sigVerifierProcess.kill()
			}
		}
	}()

	go func() {
		defer wg.Done()
		for _, shardServerProcess := range c.ShardServerProcesses {
			if shardServerProcess != nil {
				shardServerProcess.kill()
			}
		}
	}()

	go func() {
		defer wg.Done()
		if c.CoordinatorProcess != nil {
			c.CoordinatorProcess.kill()
		}
	}()

	wg.Wait()

	os.RemoveAll(c.RootDir)
}

func run(cmd *exec.Cmd, name string, startCheck string) ifrit.Process {
	sigVerifierProcess := ginkgomon.New(ginkgomon.Config{
		Command:           cmd,
		Name:              name,
		AnsiColorCode:     "",
		StartCheck:        startCheck,
		StartCheckTimeout: 0,
		Cleanup: func() {
		},
	})
	process := ifrit.Invoke(sigVerifierProcess)
	Eventually(process.Ready()).Should(BeClosed())
	return process
}

func constructConfigFilePath(rootDir, name, endpoint string) string {
	return path.Join(rootDir, name+"-"+endpoint+"-config.yaml")
}

func createConfigFile(config interface{}, templateFilePath, outputFilePath string) {
	tmpl, err := template.ParseFiles(templateFilePath)
	Expect(err).ShouldNot(HaveOccurred())

	var renderedConfig bytes.Buffer
	err = tmpl.Execute(&renderedConfig, config)
	Expect(err).ShouldNot(HaveOccurred())

	outputFile, err := os.Create(outputFilePath)
	Expect(err).ShouldNot(HaveOccurred())
	defer outputFile.Close()

	_, err = outputFile.Write(renderedConfig.Bytes())
	Expect(err).ShouldNot(HaveOccurred())
}
