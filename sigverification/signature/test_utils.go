package signature

func NewSignerVerifier(scheme Scheme) (TxSigner, TxVerifier) {
	txSigner, txVerifier, _ := newTxSignerVerifier(scheme)

	return txSigner, txVerifier
}

func NewSignerPubKey(scheme Scheme) (TxSigner, PublicKey) {
	txSigner, txVerifier, _ := newTxSignerVerifier(scheme)

	return txSigner, txVerifier.publicKey()
}
