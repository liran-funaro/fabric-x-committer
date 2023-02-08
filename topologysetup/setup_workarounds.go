package topologysetup

import (
	"fmt"
	"path/filepath"

	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/api"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/fabricconfig"
	"github.com/hyperledger/fabric/bccsp/factory"
	"github.ibm.com/decentralized-trust-research/fts-sc/integration/nwo/fabric/tss"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"gopkg.in/yaml.v3"
)

type platformFactory struct{}

func NewCustomPlatformFactory() *platformFactory {
	return &platformFactory{}
}

func (f platformFactory) Name() string {
	return "fabric"
}

func (f platformFactory) New(registry api.Context, t api.Topology, builder api.Builder) api.Platform {
	p := fabric.NewPlatform(&EnhancedRegistry{registry}, t, builder)
	p.Network.AddExtension(tss.NewExtension(p))
	return p
}

type OrdererClientProfileGenerator struct {
	rootDir string
	prefix  string
}

func NewConnectionProfileGenerator(rootDir, topologyName string) *OrdererClientProfileGenerator {
	return &OrdererClientProfileGenerator{
		rootDir: rootDir,
		prefix:  "fabric." + topologyName,
	}
}

func (g *OrdererClientProfileGenerator) GenerateOrdererClientProfiles(peerMap map[OrgName][]PeerName) error {
	for org, peers := range peerMap {
		for _, peer := range peers {
			filePath := filepath.Join(g.rootDir, g.prefix, "peers", fmt.Sprintf("%s.%s", org, peer), "profile.yaml")
			profile, err := g.connectionProfile(org, peer)
			if err != nil {
				return err
			}
			content, err := yaml.Marshal(profile)
			if err != nil {
				return err
			}
			return utils.WriteFile(filePath, content)
		}
	}
	return nil
}

func (g *OrdererClientProfileGenerator) connectionProfile(peerOrg, peerName string) (*connection.OrdererConnectionProfile, error) {
	content, err := utils.ReadFile(g.peerConfigPath(peerOrg, peerName))
	if err != nil {
		return nil, err
	}
	core := fabricconfig.Core{}
	if err = yaml.Unmarshal(content, &core); err != nil {
		return nil, err
	}

	return &connection.OrdererConnectionProfile{
		RootCAPaths: []string{g.caCertBundlePath()},
		MSPDir:      core.Peer.MSPConfigPath,
		MSPID:       core.Peer.LocalMSPID,
		BCCSP:       mapBccsp(core.Peer.BCCSP),
	}, nil
}

func (g *OrdererClientProfileGenerator) peerConfigPath(peerOrg, peerName string) string {
	return filepath.Join(g.rootDir, g.prefix, "peers", fmt.Sprintf("%s.%s", peerOrg, peerName), "core.yaml")
}

func (g *OrdererClientProfileGenerator) caCertBundlePath() string {
	return filepath.Join(g.rootDir, g.prefix, "crypto", "ca-certs.pem")
}

func mapBccsp(bccsp *fabricconfig.BCCSP) *factory.FactoryOpts {
	if bccsp == nil {
		return nil
	}
	return &factory.FactoryOpts{
		Default: bccsp.Default,
		SW:      mapSoftwareProvider(bccsp.SW),
	}
}

func mapSoftwareProvider(sw *fabricconfig.SoftwareProvider) *factory.SwOpts {
	if sw == nil {
		return nil
	}
	return &factory.SwOpts{
		Security: sw.Security,
		Hash:     sw.Hash,
	}
}

type EnhancedRegistry struct {
	api.Context
}

//TODO
func (r *EnhancedRegistry) ReservePort() uint16 {
	return r.Context.ReservePort() + 7050
}
