package fsc

import (
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/api"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fsc"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fsc/node"
	sdk "github.com/hyperledger-labs/fabric-smart-client/platform/fabric/sdk"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup"
)

func CreateTopology(config *Config, provider Provider) api.Topology {

	fscTopology := fsc.NewTopology()

	templates := map[string]*node.Node{}
	for _, nodeConfig := range config.Nodes {
		if _, ok := templates[nodeConfig.Template]; nodeConfig.Template != "" && !ok {
			templates[nodeConfig.Template] = fscTopology.NewTemplate(nodeConfig.Template)
		}
	}
	fscTopology.Templates = fsc.Templates{Node: nodeTemplate}

	for _, nodeConfig := range config.Nodes {
		var node *node.Node
		if template, ok := templates[nodeConfig.Template]; ok {
			node = fscTopology.AddNodeFromTemplate(nodeConfig.Name, template)
		} else {
			node = fscTopology.AddNodeByName(nodeConfig.Name)
		}
		node.AddOptions(fabric.WithOrganization(nodeConfig.Organization))
		for _, responder := range nodeConfig.Responders {
			node.RegisterResponder(provider.GetView(responder.Responder), provider.GetView(responder.Initiator))
		}
		for _, viewFactory := range nodeConfig.ViewFactories {
			node.RegisterViewFactory(viewFactory.Name, provider.GetViewFactory(viewFactory.Factory))
		}
	}

	//fscTopology.SetBootstrapNode(fscTopology.AddNodeByName("lib-p2p-bootstrap-node"))
	fscTopology.AddSDK(&sdk.SDK{})
	return fscTopology
}

type fscPlatformFactory struct {
	rootDir   string
	peerPorts map[topologysetup.NodeID]topologysetup.NodeConfig
}

func NewPlatformFactory(rootDir string, peerPorts map[topologysetup.NodeID]topologysetup.NodeConfig) *fscPlatformFactory {
	return &fscPlatformFactory{rootDir, peerPorts}
}

func (f fscPlatformFactory) Name() string {
	return "fsc"
}

func (f fscPlatformFactory) New(registry api.Context, t api.Topology, builder api.Builder) api.Platform {
	p := fsc.NewPlatform(&topologysetup.EnhancedRegistry{registry, f.rootDir, f.peerPorts, nil}, t, builder)
	return p
}
