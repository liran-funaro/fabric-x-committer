package signature

import "github.ibm.com/distributed-trust-research/scalable-committer/token"

// Dummy Factory

type dummyFactory struct{}

func (f *dummyFactory) newSignerVerifier() (TxSigner, TxVerifier, error) {
	return &dummyTxSignerVerifier{}, &dummyTxSignerVerifier{}, nil
}

func (f *dummyFactory) newVerifier(key []byte) (TxVerifier, error) {
	return &dummyTxSignerVerifier{}, nil
}

// Dummy signer/verifier

type dummyTxSignerVerifier struct{}

func (v *dummyTxSignerVerifier) publicKey() []byte {
	return nil
}

func (v *dummyTxSignerVerifier) SignTx([]SerialNumber) (Signature, error) {
	return []byte{}, nil
}

func (v *dummyTxSignerVerifier) VerifyTx(*token.Tx) error {
	return nil
}
