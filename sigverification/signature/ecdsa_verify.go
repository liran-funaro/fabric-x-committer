package signature

import (
	"crypto/ecdsa"

	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/crypto"
)

// ECDSA Factory

type EcdsaVerifierFactory struct{}

func (f *EcdsaVerifierFactory) NewVerifier(verificationKey []byte) (TxVerifier, error) {
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

func (v *ecdsaTxVerifier) VerifyTx(tx *token.Tx) error {
	return crypto.VerifyMessage(v.verificationKey, SignatureData(tx.GetSerialNumbers(), tx.GetOutputs()), tx.GetSignature())
}
