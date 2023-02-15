package fabric

import (
	"path/filepath"

	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/fabricconfig"
	"github.com/hyperledger/fabric/bccsp/factory"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"gopkg.in/yaml.v3"
)

type OrdererClientProfileGenerator struct {
	outputDir string
	rootDir   string
	prefix    string
}

func NewConnectionProfileGenerator(outputDir, rootDir, topologyName string) *OrdererClientProfileGenerator {
	return &OrdererClientProfileGenerator{
		outputDir: outputDir,
		rootDir:   rootDir,
		prefix:    "fabric." + topologyName,
	}
}

func (g *OrdererClientProfileGenerator) GenerateOrdererClientProfiles(peers []topologysetup.Node) error {
	for _, peer := range peers {
		profile, err := g.connectionProfile(peer.ID())
		if err != nil {
			return err
		}
		content, err := yaml.Marshal(profile)
		if err != nil {
			return err
		}
		return utils.WriteFile(g.peerProfilePath(peer.ID()), content)
	}
	return nil
}

func (g *OrdererClientProfileGenerator) connectionProfile(peerID topologysetup.NodeID) (*connection.OrdererConnectionProfile, error) {
	content, err := utils.ReadFile(g.peerConfigPath(peerID))
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

func (g *OrdererClientProfileGenerator) peerProfilePath(peerID string) string {
	return filepath.Join(g.peerDir(peerID), "profile.yaml")
}

func (g *OrdererClientProfileGenerator) peerConfigPath(peerID topologysetup.NodeID) string {
	return filepath.Join(g.peerDir(peerID), "core.yaml")
}

func (g *OrdererClientProfileGenerator) peerDir(peerID topologysetup.NodeID) string {
	return filepath.Join(g.outputDir, g.prefix, "peers", peerID)
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
