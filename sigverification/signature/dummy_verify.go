package signature

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

// Dummy Factory

type dummyVerifierFactory struct{}

func (f *dummyVerifierFactory) NewVerifier(key []byte) (NsVerifier, error) {
	return &dummyVerifier{}, nil
}

// Dummy signer/verifier

type dummyVerifier struct{}

func (v *dummyVerifier) SignNs(*protoblocktx.Tx) (Signature, error) {
	return []byte{}, nil
}

func (v *dummyVerifier) VerifyNs(t *protoblocktx.Tx, nsIndex int) error {
	return nil
}
