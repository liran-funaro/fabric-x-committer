package loadgen

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
)

// TestNamespaceGeneratorKeyCreation verifies that the signers that created by the
// namespace-generator are the same given the same seed number.
func TestNamespaceGeneratorKeyCreation(t *testing.T) {
	loadConfig(t, "client-config.yaml", clientOnlyTemplate, 9922)
	cfg := ReadConfig()
	for _, sig := range []Scheme{signature.Ecdsa, signature.Bls, signature.Eddsa} {
		t.Run(sig, func(t *testing.T) {
			cfg.LoadProfile.Transaction.Signature.Scheme = sig
			namespaceGen := NewNamespaceGenerator(cfg.LoadProfile.Transaction.Signature)

			publicKeys := make([]string, 1000)
			for i := range publicKeys {
				verificationKey := namespaceGen.getSigner().GetVerificationKey()
				publicKeys[i] = string(verificationKey)

				if i > 0 {
					require.Equal(t, publicKeys[0], publicKeys[i])
				}
			}

			// changing seed for different key generation.
			namespaceGen.sigProfile.Seed = 30

			diffSignerVerfKey := namespaceGen.getSigner().GetVerificationKey()
			require.NotEqual(t, publicKeys[0], string(diffSignerVerfKey))
		})
	}
}
