package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/hyperledger-labs/fabric-smart-client/integration"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/topology"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fsc"
	"github.com/spf13/pflag"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup"
)

func main() {
	outputDir := pflag.String("output-dir", os.Getenv("PWD")+"/out", "Output dir for config tree.")
	fabBinDir := pflag.String("fab-bin-dir", os.Getenv("GOPATH")+"/src/github.com/decentralized-trust-research/scalable-committer/eval/deployments/bins/osx/", "Path to directory with the fabric executables (configtx, peer, etc.)")
	config.ParseFlags()

	os.Setenv("FAB_BINS", *fabBinDir)

	c := topologysetup.ReadConfig()

	raftTopology := CreateRaftTopology(c)
	ii, _ := integration.New(0, *outputDir, raftTopology, fsc.NewTopology())
	ii.RegisterPlatformFactory(topologysetup.NewCustomPlatformFactory(c.RootDir, c.PeerIdMap(), c.OrdererIdMap()))
	ii.DeleteOnStart = true

	ii.Generate()

	topologysetup.NewConnectionProfileGenerator(*outputDir, c.RootDir, c.Name).GenerateOrdererClientProfiles(c.Peers)

	printCommands(*outputDir, raftTopology)
}

func printCommands(rootDir string, raftTopology *topology.Topology) {
	topLevelDir := fmt.Sprintf("%s/fabric.%s/", rootDir, raftTopology.Name())
	fmt.Println("Start orderers:")

	for _, orderer := range raftTopology.Orderers {
		fmt.Printf("\t- FABRIC_CFG_PATH=%s/orderers/%s %s/orderer\n", topLevelDir, orderer.ID(), os.Getenv("FAB_BINS"))
	}

	peer, org := anyPeer(raftTopology)
	fmt.Println("Create channels:")
	for _, channel := range raftTopology.Channels {
		fmt.Printf("\t- FABRIC_CFG_PATH=%s/peers/%s CORE_PEER_MSPCONFIGPATH=%s/crypto/peerOrganizations/%s/users/Admin@%s/msp %s/peer channel create --cafile %s/crypto/ca-certs.pem -o 127.0.0.1:7054 -c %s -f %s/%s_tx.pb --tls\n",
			topLevelDir, peer.ID(), topLevelDir, org.Domain, org.Domain, os.Getenv("FAB_BINS"), topLevelDir, channel.Name, topLevelDir, channel.Name)
	}

	fmt.Println("Use the following paths as 'orderer-connection-profile':")
	for _, peer := range raftTopology.Peers {
		fmt.Printf("\t- %s/peers/%s/profile.yaml\n", topLevelDir, peer.ID())
	}
}

func anyPeer(raftTopology *topology.Topology) (*topology.Peer, *topology.Organization) {
	peer := raftTopology.Peers[0]
	for _, org := range raftTopology.Organizations {
		if org.Name == peer.Organization {
			return peer, org
		}
	}
	panic("not found")
}

func CreateRaftTopology(config *topologysetup.Config) *topology.Topology {
	fabricTopology := &topology.Topology{
		TopologyName: config.Name,
		Default:      true,
		TopologyType: "fabric",
		Logging: &topology.Logging{
			Spec:   strings.ToLower(config.LogLevel),
			Format: "'%{color}%{time:2006-01-02 15:04:05.000 MST} [%{module}] %{shortfunc} -> %{level:.4s} %{id:03x}%{color:reset} %{message}'",
		},
		Consortiums: config.Consortiums(),
		Consensus: &topology.Consensus{
			Type: "etcdraft",
		},
		SystemChannel: &topology.SystemChannel{
			Name:    "systemchannel",
			Profile: "OrgsOrdererGenesis",
		},
		Orderers: config.AllOrderers(),
		Channels: config.AllChannels(),
		Profiles: append(config.AllChannelProfiles(),
			&topology.Profile{
				Name:     "OrgsOrdererGenesis",
				Orderers: config.AllOrdererNames(),
			}),
	}
	fabricTopology.AddOrganizationsByMapping(config.PeerNameMap())
	for _, org := range config.AllOrdererOrgs() {
		fabricTopology.AddOrganization(org)
	}

	return fabricTopology
}
