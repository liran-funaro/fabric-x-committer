package generatetopology

import (
	"os"

	"github.com/hyperledger-labs/fabric-smart-client/integration"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fabric"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fsc"
)

func GenerateAll(fabricConfig *fabric.Config, fscConfig *fsc.Config, depProvider fsc.Provider, outputDir, fabBinsDir string) {

	os.Setenv("FAB_BINS", fabBinsDir)

	fabricTopology := fabric.CreateTopology(fabricConfig)
	fabric.PrintCommands(outputDir, fabricTopology)
	fscTopology := fsc.CreateTopology(fscConfig, depProvider)

	ii, _ := integration.New(0, outputDir, fabricTopology, fscTopology)
	ii.RegisterPlatformFactory(fabric.NewPlatformFactory(outputDir, fabricConfig.PeerIdMap(), fabricConfig.OrdererIdMap()))
	ii.RegisterPlatformFactory(fsc.NewPlatformFactory(outputDir, fscConfig.PeerIdMap()))
	ii.DeleteOnStart = true

	ii.Generate()

	fabric.NewConnectionProfileGenerator(outputDir, outputDir, fabricConfig.Name).GenerateOrdererClientProfiles(fabricConfig.Peers)
}
