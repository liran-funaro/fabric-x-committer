package signature

import (
	"crypto/ecdsa"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/crypto"
)

// ECDSA Factory

type EcdsaVerifierFactory struct{}

func (f *EcdsaVerifierFactory) NewVerifier(verificationKey []byte) (NsVerifier, error) {
	ecdsaVerificationKey, err := crypto.ParseVerificationKey(verificationKey)
	if err != nil {
		return nil, err
	}
	return &ecdsaTxVerifier{verificationKey: ecdsaVerificationKey}, nil
}

// ECDSA Verifier

type ecdsaTxVerifier struct {
	verificationKey *ecdsa.PublicKey
}

func (v *ecdsaTxVerifier) publicKey() []byte {
	key, _ := crypto.SerializeVerificationKey(v.verificationKey)
	return key
}

func (v *ecdsaTxVerifier) VerifyNs(tx *protoblocktx.Tx, nsIndex int) error {
	return crypto.VerifyMessage(v.verificationKey, HashTxNamespace(tx, nsIndex), tx.GetSignatures()[nsIndex])
}
