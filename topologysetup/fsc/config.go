package fsc

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type Config struct {
	SDKDriver string               `mapstructure:"sdk-driver"`
	Nodes     []Node               `mapstructure:"nodes"`
	Tracing   *connection.Endpoint `mapstructure:"tracing"`
}

type Identity string

const DefaultIdentity Identity = "default"

type Node struct {
	*topologysetup.NodeConfig `mapstructure:"config"`
	Responders                []Responder   `mapstructure:"responders"`
	ViewFactories             []ViewFactory `mapstructure:"view-factories"`
	IntermediaryIdentity      Identity      `mapstructure:"intermediary-identity"`
	OwnerIdentities           []Identity    `mapstructure:"owner-identities"`
	IssuerIdentities          []Identity    `mapstructure:"issuer-identities"`
	Auditor                   bool          `mapstructure:"auditor"`
	Certifier                 bool          `mapstructure:"certifier"`
	Endorser                  bool          `mapstructure:"endorser"`
	Bootstrap                 bool          `mapstructure:"bootstrap"`
}

func (n *Node) ID() string {
	return "fsc." + n.Name
}

type ViewFactory struct {
	Id      string `mapstructure:"id"`
	Factory Ref    `mapstructure:"factory"`
}

type Responder struct {
	Responder Ref `mapstructure:"responder"`
	Initiator Ref `mapstructure:"initiator"`
}

//func ReadConfig() *Config {
//	wrapper := new(struct {
//		Config *Config `mapstructure:"topology-setup"`
//	})
//	config.Unmarshal(wrapper, topologysetup.PortsDecoder)
//	return wrapper.Config
//}

func (c *Config) AllViewFactories() []Ref {
	factories := make([]Ref, 0)
	for _, node := range c.Nodes {
		for _, ref := range node.ViewFactories {
			factories = append(factories, ref.Factory)
		}
	}
	return factories
}

func (c *Config) AllViews() []Ref {
	views := make([]Ref, 0)
	for _, node := range c.Nodes {
		for _, ref := range node.Responders {
			views = append(views, ref.Initiator, ref.Responder)
		}
	}
	return views
}

func (c *Config) AllSDKs() []Ref {
	return []Ref{"github.ibm.com/decentralized-trust-research/fts-sc/platform/fabric/sdk/SDK"}
}

func (c *Config) PeerIdMap() map[topologysetup.NodeID]*topologysetup.NodeConfig {
	nodeIdMap := make(map[topologysetup.NodeID]*topologysetup.NodeConfig, len(c.Nodes))
	for _, node := range c.Nodes {
		nodeIdMap[node.ID()] = node.NodeConfig
	}
	return nodeIdMap
}
