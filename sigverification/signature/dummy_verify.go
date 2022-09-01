package signature

import "github.ibm.com/distributed-trust-research/scalable-committer/token"

type dummyTxSignerVerifier struct{}

func (v *dummyTxSignerVerifier) newKeys() (PublicKey, PrivateKey) {
	return []byte{}, []byte{}
}

func (v *dummyTxSignerVerifier) signTx(PrivateKey, []SerialNumber) (Signature, error) {
	return []byte{}, nil
}

func (v *dummyTxSignerVerifier) IsVerificationKeyValid(PublicKey) bool {
	return true
}

func (v *dummyTxSignerVerifier) VerifyTx(PublicKey, *token.Tx) error {
	return nil
}
