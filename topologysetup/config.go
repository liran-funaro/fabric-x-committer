package topologysetup

import (
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/topology"
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type PeerName = string
type OrgName = string
type OrdererName = string

type OrdererConfig struct {
	Name     string
	Endpoint *connection.Endpoint
}

type Config struct {
	Name       string                    `mapstructure:"name"`
	ChannelID  string                    `mapstructure:"channel-id"`
	PeerMap    map[OrgName][]PeerName    `mapstructure:"peer-map"`
	OrdererMap map[OrgName][]OrdererName `mapstructure:"orderer-map"`
	LogLevel   logging.Level             `mapstructure:"log-level"`
}

func (c *Config) AllOrdererNames() []string {
	allOrderers := make([]string, 0)
	for _, ordererNames := range c.OrdererMap {
		allOrderers = append(allOrderers, ordererNames...)
	}
	return allOrderers
}
func (c *Config) AllOrderers() []*topology.Orderer {
	allOrderers := make([]*topology.Orderer, 0)
	for org, ordererNames := range c.OrdererMap {
		for _, ordererName := range ordererNames {
			allOrderers = append(allOrderers, &topology.Orderer{
				Name:         ordererName,
				Organization: org,
			})
		}
	}
	return allOrderers
}
func (c *Config) AllOrdererOrgs() []OrgName {
	allOrgs := make([]OrgName, 0)
	for org := range c.OrdererMap {
		allOrgs = append(allOrgs, org)
	}
	return allOrgs
}

func ReadConfig() *Config {
	wrapper := new(struct {
		Config *Config `mapstructure:"topologysetup"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func init() {
	viper.SetDefault("topologysetup.name", "mytopo")
	viper.SetDefault("topologysetup.log-level", logging.Warning)
	viper.SetDefault("topologysetup.channel-id", "mychannel")
	viper.SetDefault("topologysetup.peer-map", map[OrgName][]PeerName{"Org1": {"peer0"}})
	viper.SetDefault("topologysetup.orderer-map", map[OrgName][]OrdererName{"ordererOrg1": {"orderer_1", "orderer_2"}}) // "orderer_2", "orderer_3", "orderer_4", "orderer_5", "orderer_6", "orderer_7", "orderer_8", "orderer_9", "orderer_10", "orderer_11"}})
}
