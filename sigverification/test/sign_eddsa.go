package sigverification_test

import (
	"crypto/ed25519"

	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
)

// edDSA

type eddsaTxSigner struct {
	sk ed25519.PrivateKey
}

func (b *eddsaTxSigner) SignTx(serialNumbers []token.SerialNumber, txOutputs []token.TxOutput) (signature.Signature, error) {
	h := signature.SignatureData(serialNumbers, txOutputs)
	return b.sk.Sign(nil, h, &ed25519.Options{
		Context: "Example_ed25519ctx",
	})
}

type eddsaSignerFactory struct {
	signature.EDDSAVerifierFactory
}

func (b *eddsaSignerFactory) NewKeys() (PrivateKey, signature.PublicKey) {
	pk, sk, _ := ed25519.GenerateKey(nil)
	return sk, pk
}

func (b *eddsaSignerFactory) NewSigner(key PrivateKey) (TxSigner, error) {
	return &eddsaTxSigner{key}, nil
}
