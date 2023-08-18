package sigverification_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"math/big"
	"strings"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	signature2 "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/crypto"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
)

type SignerFactory interface {
	NewSigner(key signature.PrivateKey) (TxSigner, error)
}

type TxSigner interface {
	//SignTx signs a message and returns the signature
	SignTx([]token.SerialNumber, []token.TxOutput) (signature.Signature, error)
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

func (f *dummySignerFactory) NewSigner(signature.PrivateKey) (TxSigner, error) {
	return &dummyTxSigner{}, nil
}

type dummyTxSigner struct{}

func (s *dummyTxSigner) SignTx([]token.SerialNumber, []token.TxOutput) (signature.Signature, error) {
	return []byte{}, nil
}

type eddsaTxSigner struct {
	sk ed25519.PrivateKey
}

func (b *eddsaTxSigner) SignTx(serialNumbers []token.SerialNumber, txOutputs []token.TxOutput) (signature.Signature, error) {
	h := signature2.SignatureData(serialNumbers, txOutputs)
	return b.sk.Sign(nil, h, &ed25519.Options{
		Context: "Example_ed25519ctx",
	})
}

type eddsaSignerFactory struct{}

func (b *eddsaSignerFactory) NewSigner(key signature.PrivateKey) (TxSigner, error) {
	return &eddsaTxSigner{key}, nil
}

type blsTxSigner struct {
	sk *big.Int
}

func (b *blsTxSigner) SignTx(serialNumbers []token.SerialNumber, txOutputs []token.TxOutput) (signature.Signature, error) {
	h := signature2.SignatureData(serialNumbers, txOutputs)

	g1h, err := bn254.HashToG1(h, []byte(signature2.BLS_HASH_PREFIX))
	if err != nil {
		panic(err)
	}

	sig := g1h.ScalarMultiplication(&g1h, b.sk).Bytes()
	return sig[:], nil
}

type blsSignerFactory struct{}

func (b *blsSignerFactory) NewSigner(key signature.PrivateKey) (TxSigner, error) {
	sk := big.NewInt(0)
	sk.SetBytes(key)

	return &blsTxSigner{sk}, nil
}

type ecdsaSignerFactory struct{}

func (f *ecdsaSignerFactory) NewSigner(key signature.PrivateKey) (TxSigner, error) {
	signingKey, err := crypto.ParseSigningKey(key)
	if err != nil {
		return nil, err
	}
	return &ecdsaTxSigner{signingKey: signingKey}, nil
}

type ecdsaTxSigner struct {
	signingKey *ecdsa.PrivateKey
}

func (s *ecdsaTxSigner) SignTx(inputs []token.SerialNumber, outputs []token.TxOutput) (signature.Signature, error) {
	return crypto.SignMessage(s.signingKey, signature2.SignatureData(inputs, outputs))
}
