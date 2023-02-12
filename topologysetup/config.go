package topologysetup

import (
	"reflect"
	"strings"

	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/api"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/network"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/topology"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type NodeName = string
type NodeID = string
type OrgName = string

type NodeConfig struct {
	Name         NodeName  `mapstructure:"name"`
	Organization OrgName   `mapstructure:"organization"`
	Host         string    `mapstructure:"host"`
	Ports        api.Ports `mapstructure:"ports"`
}

func (c *NodeConfig) ID() NodeID {
	return (&topology.Peer{Name: c.Name, Organization: c.Organization}).ID()
}

const (
	channelProfile = "OrgsChannel"
	consortiumName = "SampleConsortium"
)

type Config struct {
	Name       string        `mapstructure:"name"`
	RootDir    string        `mapstructure:"root-dir"`
	ChannelIDs []string      `mapstructure:"channel-ids"`
	Peers      []NodeConfig  `mapstructure:"peers"`
	Orderers   []NodeConfig  `mapstructure:"orderers"`
	LogLevel   logging.Level `mapstructure:"log-level"`
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
	return []*topology.Profile{{
		Name:          channelProfile,
		Consortium:    consortiumName,
		Organizations: c.AllPeerOrgs(),
		Policies: []*topology.Policy{
			fabric.ImplicitMetaReaders,
			fabric.ImplicitMetaWriters,
			fabric.ImplicitMetaAdmins,
			fabric.ImplicitMetaLifecycleEndorsement,
			fabric.ImplicitMetaEndorsement,
		},
	}}
}

func (c *Config) AllOrdererNames() []NodeName {
	orderers := make([]string, len(c.Orderers))
	for i, orderer := range c.Orderers {
		orderers[i] = orderer.Name
	}
	return orderers
}

func (c *Config) PeerNameMap() map[OrgName][]NodeName {
	peers := make(map[OrgName][]NodeName, len(c.Peers))
	for _, peer := range c.Peers {
		var org []NodeName
		if m, ok := peers[peer.Organization]; ok {
			org = m
		} else {
			org = make([]NodeName, 0)
		}
		peers[peer.Organization] = append(org, peer.Name)
	}
	return peers
}
func (c *Config) PeerIdMap() map[NodeID]NodeConfig {
	return nodeIdMap(c.Peers)
}

func (c *Config) OrdererIdMap() map[NodeID]NodeConfig {
	return nodeIdMap(c.Orderers)
}

func nodeIdMap(nodes []NodeConfig) map[NodeID]NodeConfig {
	nodeIdMap := make(map[NodeID]NodeConfig, len(nodes))
	for _, node := range nodes {
		nodeIdMap[node.ID()] = node
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
func (c *Config) AllOrdererOrgs() []OrgName {
	return allUniqueOrgs(c.Orderers)
}

func (c *Config) AllPeerOrgs() []OrgName {
	return allUniqueOrgs(c.Peers)
}

func allUniqueOrgs(configs []NodeConfig) []OrgName {
	orgMap := make(map[OrgName]bool, 0)
	for _, orderer := range configs {
		orgMap[orderer.Organization] = true
	}
	orgs := make([]OrgName, 0, len(orgMap))
	for org := range orgMap {
		orgs = append(orgs, org)
	}
	return orgs
}

func ReadConfig() *Config {
	wrapper := new(struct {
		Config *Config `mapstructure:"topology-setup"`
	})
	config.Unmarshal(wrapper, portsDecoder)
	return wrapper.Config
}

var portNameMap = createPortNameMap()

func createPortNameMap() map[string]api.PortName {
	nameMap := make(map[string]api.PortName)
	for _, portName := range append(network.PeerPortNames(), network.OrdererPortNames()...) {
		nameMap[strings.ToLower(string(portName))] = portName
	}
	return nameMap
}

func portsDecoder(dataType reflect.Type, targetType reflect.Type, rawData interface{}) (interface{}, bool, error) {
	if targetType != reflect.TypeOf(api.Ports{}) {
		return rawData, false, nil
	}
	if dataType.Kind() != reflect.Map {
		return rawData, false, nil
	}
	result := api.Ports{}
	for portNameRaw, portRaw := range rawData.(map[string]interface{}) {
		port, ok := portRaw.(int)
		if !ok {
			return nil, false, errors.Errorf("could not deserialize %s as uint16", portRaw)
		}
		if port < 0 {
			return nil, false, errors.Errorf("cannot assign %d as uint16 because it is greater than zero", port)
		}
		portName, ok := portNameMap[portNameRaw]
		if !ok {
			return nil, false, errors.Errorf("could not deserialize %s as port name", portNameRaw)
		}
		result[portName] = uint16(port)
	}
	return result, true, nil
}

func init() {
	viper.SetDefault("topology-setup.name", "mytopo")
	viper.SetDefault("topology-setup.root-dir", "/root/config/")
	viper.SetDefault("topology-setup.log-level", logging.Warning)
	viper.SetDefault("topology-setup.channel-ids", []string{"mychannel"})
	viper.SetDefault("topology-setup.peers", []NodeConfig{
		{Organization: "Org1", Name: "peer0", Host: "tokentestbed3.sl.cloud9.ibm.com", Ports: api.Ports{network.ListenPort: 7000, network.ChaincodePort: 7001, network.ProfilePort: 7002, network.OperationsPort: 7003, network.EventsPort: 7004, network.P2PPort: 7005, network.WebPort: 7006}},
	})
	viper.SetDefault("topology-setup.orderers", []NodeConfig{
		{Organization: "ordererOrg1", Name: "orderer_1", Host: "tokentestbed1.sl.cloud9.ibm.com", Ports: api.Ports{network.ListenPort: 7050, network.ProfilePort: 7051, network.OperationsPort: 7052, network.ClusterPort: 7053}},
		{Organization: "ordererOrg1", Name: "orderer_2", Host: "tokentestbed2.sl.cloud9.ibm.com", Ports: api.Ports{network.ListenPort: 8050, network.ProfilePort: 8051, network.OperationsPort: 8052, network.ClusterPort: 8053}},
	}) // "orderer_2", "orderer_3", "orderer_4", "orderer_5", "orderer_6", "orderer_7", "orderer_8", "orderer_9", "orderer_10", "orderer_11"}})
}
