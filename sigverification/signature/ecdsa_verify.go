package signature

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/crypto"
)

type ecdsaTxSignerVerifier struct{}

func (v *ecdsaTxSignerVerifier) newKeys() (PublicKey, PrivateKey) {
	publicKey, privateKey, _ := crypto.NewECDSAKeys()
	return publicKey, privateKey
}

func (v *ecdsaTxSignerVerifier) signTx(signingKey PrivateKey, inputs []SerialNumber) (Signature, error) {
	return crypto.SignMessage(signingKey, signatureData(inputs))
}

func (v *ecdsaTxSignerVerifier) IsVerificationKeyValid(key PublicKey) bool {
	//TODO: Check that it is a valid ECDSA key
	return len(key) > 0
}

func (v *ecdsaTxSignerVerifier) VerifyTx(verificationKey PublicKey, tx *token.Tx) error {
	return crypto.VerifyMessage(verificationKey, signatureData(tx.GetSerialNumbers()), tx.GetSignature())
}
