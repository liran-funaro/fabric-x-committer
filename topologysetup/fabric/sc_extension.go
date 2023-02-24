package fabric

import (
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/topology"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup"
)

type Extension struct {
	p *fabric.Platform
	c *topologysetup.NodeConfig
}

func NewExtension(p *fabric.Platform, c *topologysetup.NodeConfig) *Extension {
	return &Extension{p, c}
}

func (e *Extension) CheckTopology() {
	t := e.p.Topology()
	var peerChannels []*topology.PeerChannel
	for _, channel := range t.Channels {
		peerChannels = append(peerChannels, &topology.PeerChannel{Name: channel.Name, Anchor: true})
	}
	p := &topology.Peer{
		Name:           e.c.Name,
		Organization:   e.c.Organization,
		Type:           topology.FabricPeer,
		Bootstrap:      false,
		ExecutablePath: "",
		Channels:       peerChannels,
		Usage:          "delivery",
		SkipInit:       true,
		SkipRunning:    true,
		TLSDisabled:    true,
		//Hostname:       "scalable-committer",
	}
	e.p.Network.AppendPeer(p)
}

func (e *Extension) GenerateArtifacts() {}

func (e *Extension) PostRun(bool) {}
