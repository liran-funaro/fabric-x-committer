package fsc

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup"
)

type Config struct {
	Nodes []Node `mapstructure:"nodes"`
}

type Node struct {
	topologysetup.NodeConfig `mapstructure:"config"`
	Template                 string        `mapstructure:"template"`
	Responders               []Responder   `mapstructure:"responders"`
	ViewFactories            []ViewFactory `mapstructure:"view-factories"`
}

func (n *Node) ID() string {
	return "fsc." + n.Name
}

type ViewFactory struct {
	Name    string `mapstructure:"name"`
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
	return []Ref{"github.com/hyperledger-labs/fabric-smart-client/platform/fabric/sdk/SDK"}
}

func (c *Config) PeerIdMap() map[topologysetup.NodeID]topologysetup.NodeConfig {
	nodeIdMap := make(map[topologysetup.NodeID]topologysetup.NodeConfig, len(c.Nodes))
	for _, node := range c.Nodes {
		nodeIdMap[node.ID()] = node.NodeConfig
	}
	return nodeIdMap
}
