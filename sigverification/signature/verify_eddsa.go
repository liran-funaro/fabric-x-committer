package signature

import (
	"crypto/ed25519"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

// edDSA

type EDDSAVerifierFactory struct{}

func (f *EDDSAVerifierFactory) NewVerifier(verificationKey []byte) (TxVerifier, error) {
	return &edDSATxVerifier{pk: verificationKey}, nil
}

// edDSA Verifier

type edDSATxVerifier struct {
	pk ed25519.PublicKey
}

func (v *edDSATxVerifier) VerifyTx(tx *protoblocktx.Tx) error {
	digest := HashTx(tx)
	return ed25519.VerifyWithOptions(v.pk, digest, tx.GetSignature(), &ed25519.Options{
		Context: "Example_ed25519ctx",
	})
}
