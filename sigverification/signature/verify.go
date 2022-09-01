package signature

import "github.ibm.com/distributed-trust-research/scalable-committer/token"

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

//txSigner is used only for testing purposes
type txSigner interface {
	//NewKeys generates a set of public/private keys based on the scheme implemented (e.g. RSA)
	newKeys() (PublicKey, PrivateKey)

	//SignTx signs a message and returns the signature
	signTx(PrivateKey, []SerialNumber) (Signature, error)
}

type TxVerifier interface {
	//IsVerificationKeyValid checks if a verification key is non-empty and valid, according to the scheme implemented (e.g. RSA)
	IsVerificationKeyValid(PublicKey) bool

	//VerifyTx verifies a signature of a transaction as signed by signTx
	VerifyTx(PublicKey, *token.Tx) error
}

//NewTxVerifier creates a new TX verifier according to the implementation scheme
func NewTxVerifier(scheme Scheme) TxVerifier {
	return newTxSignerVerifier(scheme)
}

func newTxSignerVerifier(scheme Scheme) interface {
	TxVerifier
	txSigner
} {
	switch scheme {
	case Ecdsa:
		return &ecdsaTxSignerVerifier{}
	case NoScheme:
		return &dummyTxSignerVerifier{}
	default:
		panic("scheme not supported")
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
