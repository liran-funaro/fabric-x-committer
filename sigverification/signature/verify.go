package signature

import (
	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

type Message = []byte
type SerialNumber = []byte
type Signature = []byte
type PublicKey = []byte
type PrivateKey = []byte

type Scheme = int

const (
	NoScheme Scheme = iota
	Ecdsa
)

type cryptoFactory interface {
	newSignerVerifier() (TxSigner, TxVerifier, error)
	newVerifier([]byte) (TxVerifier, error)
}

//TxSigner is used only for testing purposes
type TxSigner interface {
	//SignTx signs a message and returns the signature
	SignTx([]SerialNumber) (Signature, error)
}

type TxVerifier interface {
	//publicKey returns the serialized verification key for testing purposes only
	publicKey() []byte
	//VerifyTx verifies a signature of a transaction as signed by SignTx
	VerifyTx(*token.Tx) error
}

var cryptoFactories = map[Scheme]cryptoFactory{
	Ecdsa:    &ecdsaFactory{},
	NoScheme: &dummyFactory{},
}

//NewTxVerifier creates a new TX verifier according to the implementation scheme
func NewTxVerifier(scheme Scheme, key []byte) (TxVerifier, error) {
	if factory, ok := cryptoFactories[scheme]; ok {
		return factory.newVerifier(key)
	} else {
		return nil, errors.New("scheme not supported")
	}
}

func newTxSignerVerifier(scheme Scheme) (TxSigner, TxVerifier, error) {
	if factory, ok := cryptoFactories[scheme]; ok {
		return factory.newSignerVerifier()
	} else {
		return nil, nil, errors.New("scheme not supported")
	}
}

func signatureData(inputs []SerialNumber) Message {
	if len(inputs) == 0 {
		return []byte{}
	}
	var data Message
	for _, input := range inputs {
		//TODO:adc Should we add separators between the inputs?
		data = append(data, input...)
	}
	return data
}
