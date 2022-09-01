package signature

import "github.ibm.com/distributed-trust-research/scalable-committer/token"

func NewSignerVerifier(scheme Scheme) (func([]SerialNumber) *token.Tx, PublicKey) {
	signer := newTxSignerVerifier(scheme)
	verificationKey, signingKey := signer.newKeys()

	signFunc := func(inputs []SerialNumber) *token.Tx {
		signature, _ := signer.signTx(signingKey, inputs)
		return &token.Tx{SerialNumbers: inputs, Signature: signature}
	}
	return signFunc, verificationKey
}
