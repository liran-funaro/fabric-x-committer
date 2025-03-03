package sigtest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/policy"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
)

// MakePolicyAndNsSigner generates a policyItem and NsSigner.
func MakePolicyAndNsSigner(
	t *testing.T,
	ns string,
) (*protoblocktx.PolicyItem, *NsSigner) {
	t.Helper()
	factory := NewSignatureFactory(signature.Ecdsa)
	signingKey, verificationKey := factory.NewKeys()
	txSigner, err := factory.NewSigner(signingKey)
	require.NoError(t, err)
	p := policy.MakePolicy(t, ns, &protoblocktx.NamespacePolicy{
		PublicKey: verificationKey,
		Scheme:    signature.Ecdsa,
	})
	return p, txSigner
}
