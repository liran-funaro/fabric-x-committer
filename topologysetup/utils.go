package topologysetup

import (
	"reflect"
	"strings"

	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/api"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/network"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/topology"
	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type NodeName = string
type NodeID = string
type OrgName = string

var logger = logging.New("topologysetup")

type Node struct {
	*NodeConfig `mapstructure:"config"`
}

type NodeConfig struct {
	Name         NodeName  `mapstructure:"name"`
	Organization OrgName   `mapstructure:"organization"`
	Host         string    `mapstructure:"host"`
	Ports        api.Ports `mapstructure:"ports"`
}

func (c *NodeConfig) ID() NodeID {
	return (&topology.Peer{Name: c.Name, Organization: c.Organization}).ID()
}

//TODO: Temporary workaround until we refactor NWO
type EnhancedRegistry struct {
	api.Context
	TopRootDir   string
	PeerPorts    map[NodeID]*NodeConfig
	OrdererPorts map[NodeID]*NodeConfig
}

func (r *EnhancedRegistry) PortsByPeerID(_ string, id NodeID) api.Ports {
	if c, ok := r.PeerPorts[id]; ok {
		return c.Ports
	}
	logger.Warnf("Ports for peer %s not found.", id)
	return api.Ports{}
}

func (r *EnhancedRegistry) PortsByOrdererID(_ string, id NodeID) api.Ports {
	if c, ok := r.OrdererPorts[id]; ok {
		return c.Ports
	}
	logger.Warnf("Ports for orderer %s not found.", id)
	return api.Ports{}
}

func (r *EnhancedRegistry) HostByPeerID(_ string, id NodeID) string {
	if c, ok := r.PeerPorts[id]; ok {
		return c.Host
	}
	logger.Warnf("Host for peer %s not found.", id)
	return ""
}

func (r *EnhancedRegistry) HostByOrdererID(_ string, id NodeID) string {
	if c, ok := r.OrdererPorts[id]; ok {
		return c.Host
	}
	logger.Warnf("Host for orderer %s not found.", id)
	return ""
}

var portNameMap = createPortNameMap()

func createPortNameMap() map[string]api.PortName {
	nameMap := make(map[string]api.PortName)
	for _, portName := range append(network.PeerPortNames(), network.OrdererPortNames()...) {
		nameMap[strings.ToLower(string(portName))] = portName
	}
	return nameMap
}

func PortsDecoder(dataType reflect.Type, targetType reflect.Type, rawData interface{}) (interface{}, bool, error) {
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
