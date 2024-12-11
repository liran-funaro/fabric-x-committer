package workload

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
)

var namespacesUnderTest = []types.NamespaceID{0, 1, 2}

// TestNamespaceGeneratorKeyCreation verifies that the signers that created by the
// namespace-generator are the same given the same seed number.
func TestNamespaceGeneratorKeyCreation(t *testing.T) {
	for _, sig := range []Scheme{signature.Ecdsa, signature.Bls, signature.Eddsa} {
		t.Run(sig, func(t *testing.T) {
			mainNsTx, err := NewNamespaceGenerator(SignatureProfile{
				Scheme:     sig,
				Namespaces: namespacesUnderTest,
				Seed:       10,
			}).CreateNamespaces()
			require.NoError(t, err)
			mainRw := getReadWritesFromNamespaceTx(t, mainNsTx)

			// Check for consistency.
			for range 1000 {
				nsTx, err2 := NewNamespaceGenerator(SignatureProfile{
					Scheme:     sig,
					Namespaces: namespacesUnderTest,
					Seed:       10,
				}).CreateNamespaces()
				require.NoError(t, err2)
				curRw := getReadWritesFromNamespaceTx(t, nsTx)
				for i, rw := range mainRw {
					require.Equal(t, rw.Value, curRw[i].Value)
				}
			}

			// changing seed for different key generation.
			nsTx, err := NewNamespaceGenerator(SignatureProfile{
				Scheme:     sig,
				Namespaces: namespacesUnderTest,
				Seed:       20,
			}).CreateNamespaces()
			require.NoError(t, err)
			curRw := getReadWritesFromNamespaceTx(t, nsTx)
			for i, rw := range mainNsTx.Namespaces[0].ReadWrites {
				require.NotEqual(t, rw.Value, curRw[i].Value)
			}
		})
	}
}

func getReadWritesFromNamespaceTx(t *testing.T, tx *protoblocktx.Tx) []*protoblocktx.ReadWrite {
	require.NotNil(t, tx)
	require.Len(t, tx.Namespaces, 1)
	require.Len(t, tx.Namespaces[0].ReadWrites, len(namespacesUnderTest))
	return tx.Namespaces[0].ReadWrites
}
