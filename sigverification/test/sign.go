package sigverification_test

import (
	"crypto/ecdsa"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/crypto"
)

type PrivateKey = []byte

type SignerFactory interface {
	signature.VerifierFactory
	NewKeys() (PrivateKey, signature.PublicKey)
	NewSigner(key PrivateKey) (TxSigner, error)
}

type TxSigner interface {
	//SignTx signs a message and returns the signature
	SignTx([]token.SerialNumber, []token.TxOutput) (signature.Signature, error)
}

var cryptoFactories = map[signature.Scheme]SignerFactory{
	signature.Ecdsa:    &ecdsaSignerFactory{},
	signature.NoScheme: &dummySignerFactory{},
}

func GetSignatureFactory(scheme signature.Scheme) SignerFactory {
	factory, ok := cryptoFactories[scheme]
	if !ok {
		panic("scheme not supported")
	}
	return factory
}

// ECDSA

type ecdsaSignerFactory struct {
	signature.EcdsaVerifierFactory
}

func (f *ecdsaSignerFactory) NewKeys() (PrivateKey, signature.PublicKey) {
	privateKey, err := crypto.NewECDSAKey()
	if err != nil {
		return nil, nil
	}
	serializedPrivateKey, err := crypto.SerializeSigningKey(privateKey)
	if err != nil {
		return nil, nil
	}
	serializedPublicKey, err := crypto.SerializeVerificationKey(&privateKey.PublicKey)
	if err != nil {
		return nil, nil
	}
	return serializedPrivateKey, serializedPublicKey
}

func (f *ecdsaSignerFactory) NewSigner(key PrivateKey) (TxSigner, error) {
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
	return crypto.SignMessage(s.signingKey, signature.SignatureData(inputs, outputs))
}

// Dummy

type dummySignerFactory struct {
	signature.EcdsaVerifierFactory
}

func (f *dummySignerFactory) NewKeys() (PrivateKey, signature.PublicKey) {
	return []byte{}, []byte{}
}

func (f *dummySignerFactory) NewSigner(key PrivateKey) (TxSigner, error) {
	return &dummyTxSigner{}, nil
}

type dummyTxSigner struct {
}

func (s *dummyTxSigner) SignTx([]token.SerialNumber, []token.TxOutput) (signature.Signature, error) {
	return []byte{}, nil
}
