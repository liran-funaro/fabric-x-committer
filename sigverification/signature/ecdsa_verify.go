package signature

import (
	"crypto/ecdsa"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/crypto"
)

// ECDSA Factory

type ecdsaFactory struct{}

func (f *ecdsaFactory) newSignerVerifier() (TxSigner, TxVerifier, error) {
	privateKey, err := crypto.NewECDSAKey()
	if err != nil {
		return nil, nil, err
	}
	return &ecdsaTxSigner{signingKey: privateKey}, &ecdsaTxVerifier{verificationKey: &privateKey.PublicKey}, nil
}

func (f *ecdsaFactory) newVerifier(verificationKey []byte) (TxVerifier, error) {
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
	return crypto.VerifyMessage(v.verificationKey, signatureData(tx.GetSerialNumbers()), tx.GetSignature())
}

// ECDSA Signer

type ecdsaTxSigner struct {
	signingKey *ecdsa.PrivateKey
}

func (s *ecdsaTxSigner) SignTx(inputs []SerialNumber) (Signature, error) {
	return crypto.SignMessage(s.signingKey, signatureData(inputs))
}
