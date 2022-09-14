package signature

import "github.ibm.com/distributed-trust-research/scalable-committer/token"

// Dummy Factory

type dummyVerifierFactory struct{}

func (f *dummyVerifierFactory) NewVerifier(key []byte) (TxVerifier, error) {
	return &dummyVerifier{}, nil
}

// Dummy signer/verifier

type dummyVerifier struct{}

func (v *dummyVerifier) SignTx([]SerialNumber) (Signature, error) {
	return []byte{}, nil
}

func (v *dummyVerifier) VerifyTx(*token.Tx) error {
	return nil
}
