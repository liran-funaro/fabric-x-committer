package fabric

import (
	"fmt"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/api"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/network"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/topology"
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"strings"
)

const (
	channelProfile = "OrgsChannel"
	consortiumName = "SampleConsortium"
)

type Config struct {
	Name        string               `mapstructure:"name"`
	OrdererType utils.ConsensusType  `mapstructure:"orderer-type"`
	ChannelIDs  []string             `mapstructure:"channel-ids"`
	Peers       []topologysetup.Node `mapstructure:"peers"`
	Sidecar     topologysetup.Node   `mapstructure:"sidecar"`
	Orderers    []topologysetup.Node `mapstructure:"orderers"`
	LogLevel    logging.Level        `mapstructure:"log-level"`
}

func (c *Config) AllChannels() []*topology.Channel {
	channels := make([]*topology.Channel, len(c.ChannelIDs))
	for i, channelID := range c.ChannelIDs {
		channels[i] = &topology.Channel{Name: channelID, Profile: channelProfile, Default: true}
	}
	return channels
}

func (c *Config) Consortiums() []*topology.Consortium {
	return []*topology.Consortium{{
		Name: consortiumName,
	}}
}

func (c *Config) AllChannelProfiles() []*topology.Profile {
	peerOrgs := c.AllPeerOrgs()
	peerOrgMembers := make([]string, len(peerOrgs))
	for i, peerOrg := range peerOrgs {
		peerOrgMembers[i] = fmt.Sprintf("'%sMSP.member'", peerOrg) // TODO: Find MSP name
	}
	return []*topology.Profile{{
		Name:          channelProfile,
		Consortium:    consortiumName,
		Organizations: c.AllPeerOrgs(),
		Orderers:      c.AllOrdererNames(), // TODO: AF New addition
		Policies: []*topology.Policy{
			fabric.ImplicitMetaReaders,
			fabric.ImplicitMetaWriters,
			fabric.ImplicitMetaAdmins,
			{Name: "LifecycleEndorsement", Type: "Signature", Rule: fmt.Sprintf("AND (%s)", strings.Join(peerOrgMembers, ","))},
			fabric.ImplicitMetaEndorsement,
		},
	}}
}

func (c *Config) AllOrdererNames() []topologysetup.NodeName {
	orderers := make([]string, len(c.Orderers))
	for i, orderer := range c.Orderers {
		orderers[i] = orderer.Name
	}
	return orderers
}

func (c *Config) PeerNameMap() map[topologysetup.OrgName][]topologysetup.NodeName {
	peers := make(map[topologysetup.OrgName][]topologysetup.NodeName, len(c.Peers))
	for _, peer := range c.Peers {
		var org []topologysetup.NodeName
		if m, ok := peers[peer.Organization]; ok {
			org = m
		} else {
			org = make([]topologysetup.NodeName, 0)
		}
		peers[peer.Organization] = append(org, peer.Name)
	}
	return peers
}
func (c *Config) PeerIdMap() map[topologysetup.NodeID]*topologysetup.NodeConfig {
	clients := c.Peers
	if c.Sidecar.NodeConfig != nil {
		clients = append(clients, c.Sidecar)
	}
	return nodeIdMap(clients)
}

func (c *Config) OrdererIdMap() map[topologysetup.NodeID]*topologysetup.NodeConfig {
	return nodeIdMap(c.Orderers)
}

func nodeIdMap(nodes []topologysetup.Node) map[topologysetup.NodeID]*topologysetup.NodeConfig {
	nodeIdMap := make(map[topologysetup.NodeID]*topologysetup.NodeConfig, len(nodes))
	for _, node := range nodes {
		nodeIdMap[node.ID()] = node.NodeConfig
	}
	return nodeIdMap
}

func (c *Config) AllOrderers() []*topology.Orderer {
	orderers := make([]*topology.Orderer, len(c.Orderers))
	for i, orderer := range c.Orderers {
		orderers[i] = &topology.Orderer{
			Name:         orderer.Name,
			Organization: orderer.Organization,
		}
	}
	return orderers
}
func (c *Config) AllOrdererOrgs() []topologysetup.OrgName {
	return allUniqueOrgs(c.Orderers)
}

func (c *Config) AllPeerOrgs() []topologysetup.OrgName {
	return allUniqueOrgs(c.Peers)
}

func allUniqueOrgs(configs []topologysetup.Node) []topologysetup.OrgName {
	orgMap := make(map[topologysetup.OrgName]bool, 0)
	for _, orderer := range configs {
		orgMap[orderer.Organization] = true
	}
	orgs := make([]topologysetup.OrgName, 0, len(orgMap))
	for org := range orgMap {
		orgs = append(orgs, org)
	}
	return orgs
}

//func ReadConfig() *Config {
//	wrapper := new(struct {
//		Config *Config `mapstructure:"topology-setup"`
//	})
//	config.Unmarshal(wrapper, PortsDecoder)
//	return wrapper.Config
//}

func init() {
	viper.SetDefault("topology-setup.name", "mytopo")
	viper.SetDefault("topology-setup.root-dir", "/root/config/")
	viper.SetDefault("topology-setup.log-level", logging.Warning)
	viper.SetDefault("topology-setup.channel-ids", []string{"mychannel"})
	viper.SetDefault("topology-setup.peers", []topologysetup.Node{
		{&topologysetup.NodeConfig{Organization: "Org1", Name: "peer0", Host: "tokentestbed3.sl.cloud9.ibm.com", Ports: api.Ports{network.ListenPort: 7000, network.ChaincodePort: 7001, network.ProfilePort: 7002, network.OperationsPort: 7003, network.EventsPort: 7004, network.P2PPort: 7005, network.WebPort: 7006}}},
	})
	viper.SetDefault("topology-setup.orderers", []topologysetup.Node{
		{&topologysetup.NodeConfig{Organization: "ordererOrg1", Name: "orderer_1", Host: "tokentestbed1.sl.cloud9.ibm.com", Ports: api.Ports{network.ListenPort: 7050, network.ProfilePort: 7051, network.OperationsPort: 7052, network.ClusterPort: 7053}}},
		{&topologysetup.NodeConfig{Organization: "ordererOrg1", Name: "orderer_2", Host: "tokentestbed2.sl.cloud9.ibm.com", Ports: api.Ports{network.ListenPort: 8050, network.ProfilePort: 8051, network.OperationsPort: 8052, network.ClusterPort: 8053}}},
	}) // "orderer_2", "orderer_3", "orderer_4", "orderer_5", "orderer_6", "orderer_7", "orderer_8", "orderer_9", "orderer_10", "orderer_11"}})
}
