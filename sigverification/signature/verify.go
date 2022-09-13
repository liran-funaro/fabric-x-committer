package signature

import (
	"encoding/asn1"
	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type Message = []byte
type SerialNumber = []byte
type Signature = []byte
type PublicKey = []byte
type PrivateKey = []byte

var log = logging.New("verifier")

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
	data, err := asn1.Marshal(inputs)
	if err != nil {
		log.Error("failed to serialize the inputs")
		return []byte{}
	}
	return data
}
