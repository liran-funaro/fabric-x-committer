package signer_v1

import (
	"errors"
	"strings"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/token"
)

type SignerFactory interface {
	NewSigner(key signature.PrivateKey) (TxSigner, error)
}

type TxSigner interface {
	//SignTx signs a message and returns the signature
	SignTx([]token.SerialNumber, []token.TxOutput) (signature.Signature, error)
}

var signerFactories = map[signature.Scheme]SignerFactory{
	signature.NoScheme: &dummySignerFactory{},
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
