package sigverification_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"errors"
	"math/big"
	"strings"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	signature2 "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/crypto"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
)

type SignerFactory interface {
	NewSigner(key signature.PrivateKey) (NsSigner, error)
}

type NsSigner interface {
	// SignNs signs a namespace of a transaction.
	SignNs(t *protoblocktx.Tx, nsIndex int) (signature.Signature, error)
}

var signerFactories = map[signature.Scheme]SignerFactory{
	signature.Ecdsa:    &ecdsaSignerFactory{},
	signature.NoScheme: &dummySignerFactory{},
	signature.Bls:      &blsSignerFactory{},
	signature.Eddsa:    &eddsaSignerFactory{},
}

func GetSignerFactory(scheme signature.Scheme) (SignerFactory, error) {
	if factory, ok := signerFactories[strings.ToUpper(scheme)]; ok {
		return factory, nil
	}
	return nil, errors.New("scheme not supported for keygen")
}

type dummySignerFactory struct{}

func (f *dummySignerFactory) NewSigner(signature.PrivateKey) (NsSigner, error) {
	return &dummyTxSigner{}, nil
}

type dummyTxSigner struct{}

func (s *dummyTxSigner) SignNs(tx *protoblocktx.Tx, nsIndex int) (signature.Signature, error) {
	return []byte{}, nil
}

type eddsaTxSigner struct {
	sk ed25519.PrivateKey
}

func (b *eddsaTxSigner) SignNs(tx *protoblocktx.Tx, nsIndex int) (signature.Signature, error) {
	h := signature2.HashTxNamespace(tx, nsIndex)
	return b.sk.Sign(nil, h, &ed25519.Options{
		Context: "Example_ed25519ctx",
	})
}

type eddsaSignerFactory struct{}

func (b *eddsaSignerFactory) NewSigner(key signature.PrivateKey) (NsSigner, error) {
	return &eddsaTxSigner{key}, nil
}

type blsTxSigner struct {
	sk *big.Int
}

func (b *blsTxSigner) SignNs(tx *protoblocktx.Tx, nsIndex int) (signature.Signature, error) {
	h := signature2.HashTxNamespace(tx, nsIndex)
	g1h, err := bn254.HashToG1(h, []byte(signature2.BLS_HASH_PREFIX))
	if err != nil {
		panic(err)
	}

	sig := g1h.ScalarMultiplication(&g1h, b.sk).Bytes()
	return sig[:], nil
}

type blsSignerFactory struct{}

func (b *blsSignerFactory) NewSigner(key signature.PrivateKey) (NsSigner, error) {
	sk := big.NewInt(0)
	sk.SetBytes(key)

	return &blsTxSigner{sk}, nil
}

type ecdsaSignerFactory struct{}

func (f *ecdsaSignerFactory) NewSigner(key signature.PrivateKey) (NsSigner, error) {
	signingKey, err := crypto.ParseSigningKey(key)
	if err != nil {
		return nil, err
	}
	return &ecdsaTxSigner{signingKey: signingKey}, nil
}

type ecdsaTxSigner struct {
	signingKey *ecdsa.PrivateKey
}

func (s *ecdsaTxSigner) SignNs(tx *protoblocktx.Tx, nsIndex int) (signature.Signature, error) {
	return crypto.SignMessage(s.signingKey, signature2.HashTxNamespace(tx, nsIndex))
}
