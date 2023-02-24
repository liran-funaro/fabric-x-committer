package fsc

import (
	"fmt"

	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/api"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fsc"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fsc/node"
	"github.com/hyperledger-labs/fabric-token-sdk/integration/nwo/token"
	"github.ibm.com/decentralized-trust-research/fts-sc/integration/nwo/fabric/tss"
	sdk "github.ibm.com/decentralized-trust-research/fts-sc/platform/fabric/sdk"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup"
)

const EndorserLabel = "endorser"

func CreateTopology(config *Config, provider Provider) *fsc.Topology {
	fscTopology := fsc.NewTopology()

	endorserTemplate := fscTopology.NewTemplate(EndorserLabel)
	fscTopology.Templates = fsc.Templates{Node: nodeTemplate}

	for _, nodeConfig := range config.Nodes {
		var node *node.Node
		if nodeConfig.Endorser {
			node = fscTopology.AddNodeFromTemplate(nodeConfig.Name, endorserTemplate)
		} else {
			node = fscTopology.AddNodeByName(nodeConfig.Name)
		}

		node.AddOptions(nodeConfig.AllOptions(node.Name, config.SDKDriver)...)
		for _, responder := range nodeConfig.Responders {
			node.RegisterResponder(provider.GetView(responder.Responder), provider.GetView(responder.Initiator))
		}
		for _, viewFactory := range nodeConfig.ViewFactories {
			node.RegisterViewFactory(viewFactory.Id, provider.GetViewFactory(viewFactory.Factory))
		}
		if nodeConfig.Bootstrap {
			fscTopology.SetBootstrapNode(node)
		}
	}

	fscTopology.AddSDK(&sdk.SDK{})
	return fscTopology
}

func (n *Node) AllOptions(nodeName, sdkDriver string) []node.Option {
	options := make([]node.Option, 0)
	options = append(options, fabric.WithOrganization(n.Organization))
	if n.Endorser {
		options = append(options, tss.WithShare("endorser"))
	}
	for _, ownerIdentity := range n.OwnerIdentities {
		if ownerIdentity == DefaultIdentity {
			options = append(options, token.WithDefaultOwnerIdentity(sdkDriver))
		} else {
			options = append(options, token.WithOwnerIdentity(sdkDriver, fmt.Sprintf("%s.%s", nodeName, ownerIdentity)))
		}
	}
	for _, issuerIdentity := range n.IssuerIdentities {
		if issuerIdentity == DefaultIdentity {
			options = append(options, token.WithDefaultIssuerIdentity())
		} else {
			options = append(options, token.WithIssuerIdentity(fmt.Sprintf("%s.%s", nodeName, issuerIdentity)))
		}
	}
	if n.Auditor {
		options = append(options, token.WithAuditorIdentity())
	}
	if n.Certifier {
		options = append(options, token.WithCertifierIdentity())
	}
	return options
}

type fscPlatformFactory struct {
	rootDir   string
	peerPorts map[topologysetup.NodeID]*topologysetup.NodeConfig
}

func NewPlatformFactory(rootDir string, peerPorts map[topologysetup.NodeID]*topologysetup.NodeConfig) *fscPlatformFactory {
	return &fscPlatformFactory{rootDir, peerPorts}
}

func (f fscPlatformFactory) Name() string {
	return "fsc"
}

func (f fscPlatformFactory) New(registry api.Context, t api.Topology, builder api.Builder) api.Platform {
	p := fsc.NewPlatform(&topologysetup.EnhancedRegistry{registry, f.rootDir, f.peerPorts, nil}, t, builder)
	return p
}
