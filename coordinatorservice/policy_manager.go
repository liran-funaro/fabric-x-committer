package coordinatorservice

import (
	"context"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/policy"
)

// policyManager is responsible for updating the committer policy and configuration according to the processed TXs.
type policyManager struct {
	signVerifierMgr *signatureVerifierManager
}

func (p *policyManager) updatePoliciesFromTx(
	ctx context.Context,
	namespaces []*protoblocktx.TxNamespace,
) error {
	for _, ns := range namespaces {
		if types.NamespaceID(ns.NsId) != types.MetaNamespaceID {
			continue
		}
		pd := append(policy.ListPolicyItems(ns.ReadWrites), policy.ListPolicyItems(ns.BlindWrites)...)
		if len(pd) == 0 {
			return nil
		}
		return p.updatePolicies(ctx, &protosigverifierservice.Policies{
			Policies: pd,
		})
	}
	return nil
}

func (p *policyManager) updatePolicies(
	ctx context.Context,
	policies *protosigverifierservice.Policies,
) error {
	return p.signVerifierMgr.updatePolicies(ctx, policies)
}
