package generatetopology

import (
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/api"
	"os"

	"github.com/hyperledger-labs/fabric-smart-client/integration"
	"github.com/onsi/gomega"
	token2 "github.ibm.com/decentralized-trust-research/fts-sc/integration/nwo/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fabric"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fsc"
	token3 "github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/token"
)

func GenerateAll(fabricConfig fabric.Config, fscConfig fsc.Config, depProvider fsc.Provider, outputDir, fabBinsDir string) {
	gomega.RegisterFailHandler(func(message string, callerSkip ...int) {
		panic(message)
	})
	os.Setenv("FAB_BINS", fabBinsDir)

	fabricTopology := fabric.CreateTopology(fabricConfig)
	fabric.PrintCommands(outputDir, fabricTopology)
	fscTopology := fsc.CreateTopology(fscConfig, depProvider)
	topologies := []api.Topology{fabricTopology, fscTopology}
	if len(fscConfig.Nodes) > 0 {
		tokenTopology := token3.CreateTopology(fabricTopology, fscTopology, fscConfig.SDKDriver)
		topologies = append(topologies, tokenTopology)
	}

	ii, _ := integration.New(0, outputDir, topologies...)
	ii.RegisterPlatformFactory(fabric.NewPlatformFactory(outputDir, fabricConfig.PeerIdMap(), fabricConfig.OrdererIdMap(), fabricConfig.Sidecar.NodeConfig))
	ii.RegisterPlatformFactory(fsc.NewPlatformFactory(outputDir, fscConfig.PeerIdMap()))
	if len(fscConfig.Nodes) > 0 {
		ii.RegisterPlatformFactory(token2.NewPlatformFactory())
	}
	ii.DeleteOnStart = true
	ii.Generate()

	fabric.NewConnectionProfileGenerator(outputDir, fabricConfig.Name).GenerateOrdererClientProfiles(fabricConfig.Peers)
}
