package signature

import "github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"

// Dummy Factory

type dummyVerifierFactory struct{}

func (f *dummyVerifierFactory) NewVerifier(key []byte) (TxVerifier, error) {
	return &dummyVerifier{}, nil
}

// Dummy signer/verifier

type dummyVerifier struct{}

func (v *dummyVerifier) SignTx([]token.SerialNumber, []token.TxOutput) (Signature, error) {
	return []byte{}, nil
}

func (v *dummyVerifier) VerifyTx(*token.Tx) error {
	return nil
}
