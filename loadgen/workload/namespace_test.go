package workload

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
)

var namespacesUnderTest = []types.NamespaceID{0, 1, 2, types.MetaNamespaceID}

// TestNamespaceGeneratorKeyCreation verifies that the signers that created by the
// namespace-generator are the same given the same seed number.
func TestNamespaceGeneratorKeyCreation(t *testing.T) {
	for _, sig := range []Scheme{signature.Ecdsa, signature.Bls, signature.Eddsa} {
		policyProfile := &PolicyProfile{
			NamespacePolicies: make(map[types.NamespaceID]*Policy, len(namespacesUnderTest)),
		}
		for i, ns := range namespacesUnderTest {
			policyProfile.NamespacePolicies[ns] = &Policy{
				Scheme: sig,
				Seed:   int64(i),
			}
		}
		t.Run(sig, func(t *testing.T) {
			mainNsTx, err := CreateNamespaces(policyProfile)
			require.NoError(t, err)
			mainRw := getReadWritesFromNamespaceTx(t, mainNsTx)

			// Check for consistency.
			for range 1000 {
				nsTx, err2 := CreateNamespaces(policyProfile)
				require.NoError(t, err2)
				curRw := getReadWritesFromNamespaceTx(t, nsTx)
				require.ElementsMatch(t, curRw, mainRw)
			}

			// changing seed for different key generation.
			for _, p := range policyProfile.NamespacePolicies {
				p.Seed += 100
			}
			nsTx, err := CreateNamespaces(policyProfile)
			require.NoError(t, err)
			curRw := getReadWritesFromNamespaceTx(t, nsTx)
			for _, rw := range mainRw {
				for _, cRw := range curRw {
					require.NotEqual(t, rw, cRw)
				}
			}
		})
	}
}

func getReadWritesFromNamespaceTx(t *testing.T, tx *protoblocktx.Tx) []*protoblocktx.ReadWrite {
	require.NotNil(t, tx)
	require.Len(t, tx.Namespaces, 1)
	require.Equal(t, uint32(types.MetaNamespaceID), tx.Namespaces[0].NsId)
	require.Len(t, tx.Namespaces[0].ReadWrites, len(namespacesUnderTest)-1)
	return tx.Namespaces[0].ReadWrites
}
