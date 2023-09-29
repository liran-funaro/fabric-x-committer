package signature

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

// Dummy Factory

type dummyVerifierFactory struct{}

func (f *dummyVerifierFactory) NewVerifier(key []byte) (TxVerifier, error) {
	return &dummyVerifier{}, nil
}

// Dummy signer/verifier

type dummyVerifier struct{}

func (v *dummyVerifier) SignTx(*protoblocktx.Tx) (Signature, error) {
	return []byte{}, nil
}

func (v *dummyVerifier) VerifyTx(*protoblocktx.Tx) error {
	return nil
}
