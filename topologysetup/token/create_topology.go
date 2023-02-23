package token

import (
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric/topology"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fsc"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fsc/node"
	"github.com/hyperledger-labs/fabric-token-sdk/integration/nwo/token"
	"github.com/hyperledger-labs/fabric-token-sdk/integration/nwo/token/fabric"
	topology2 "github.com/hyperledger-labs/fabric-token-sdk/integration/nwo/token/topology"
	token2 "github.ibm.com/decentralized-trust-research/fts-sc/integration/nwo/token"
	token3 "github.ibm.com/decentralized-trust-research/fts-sc/token/sdk"
	fsc2 "github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fsc"
)

func CreateTopology(fabricTopology *topology.Topology, fscTopology *fsc.Topology, sdkDriver string) *token.Topology {
	token.Drivers = append(token.Drivers, sdkDriver)

	tokenTopology := token.NewTopology()

	tms := tokenTopology.AddTMS(fscTopology.ListNodes(), fabricTopology, fabricTopology.Channels[0].Name, sdkDriver)
	tms.SetNamespace("token-chaincode")

	token2.SetTSSEndorsers(tms, fsc2.EndorserLabel, getEndorserNames(fscTopology)...)
	tms.SetTokenGenPublicParams("9999")

	auditor := getAuditor(fscTopology)
	tms.AddAuditor(auditor)
	tms.AddCertifier(auditor)
	token2.SetCustodian(tms, auditor)

	fabric.SetOrgs(tms, getPeerOrgs(fabricTopology)...)

	tokenTopology.SetSDK(fscTopology, &token3.SDK{})
	return tokenTopology
}

func getPeerOrgs(t *topology.Topology) []string {
	orgs := make([]string, 0)
	for _, peer := range t.Peers {
		orgs = append(orgs, peer.Organization)
	}
	return orgs
}

func getEndorserNames(t *fsc.Topology) []string {
	endorsers := make([]string, 0)
	for _, node := range t.Nodes {
		if shareLabel, ok := node.Options.Get("tss.shareLabel").(string); ok && shareLabel == fsc2.EndorserLabel {
			endorsers = append(endorsers, node.Name)
		}
	}
	return endorsers
}

func getAuditor(t *fsc.Topology) *node.Node {
	for _, node := range t.Nodes {
		if tokenOptions, ok := node.Options.Get("token").(*topology2.Options); ok && tokenOptions.Auditor() {
			return node
		}
	}
	return nil
}
