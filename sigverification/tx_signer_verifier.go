package sigverification

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/crypto"
)

type Message = []byte
type SerialNumber = []byte
type Signature = []byte
type PublicKey = *Key
type PrivateKey = *Key

type Scheme = int

const (
	NoScheme Scheme = iota
	Ecdsa
)

type TxSigner interface {
	//NewKeys generates a set of public/private keys based on the scheme implemented (e.g. RSA)
	//For testing purposes only
	NewKeys() (PublicKey, PrivateKey)

	//SignTx signs a message and returns the signature
	//For testing purposes only
	SignTx(PrivateKey, []SerialNumber) (Signature, error)
}

type TxVerifier interface {
	//IsVerificationKeyValid checks if a verification key is non-empty and valid, according to the scheme implemented (e.g. RSA)
	IsVerificationKeyValid(PublicKey) bool

	//VerifyTx verifies a signature of a transaction as signed by SignTx
	VerifyTx(PublicKey, *token.Tx) error
}

//NewTxSigner creates a new TX signer according to the implementation scheme
//Only used for tests
func NewTxSigner(scheme Scheme) TxSigner {
	return newTxSignerVerifier(scheme)
}

//NewTxVerifier creates a new TX verifier according to the implementation scheme
func NewTxVerifier(scheme Scheme) TxVerifier {
	return newTxSignerVerifier(scheme)
}

func newTxSignerVerifier(scheme Scheme) interface {
	TxVerifier
	TxSigner
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

type ecdsaTxSignerVerifier struct{}

func (v *ecdsaTxSignerVerifier) NewKeys() (PublicKey, PrivateKey) {
	publicKey, privateKey, _ := crypto.NewECDSAKeys()
	return &Key{SerializedBytes: publicKey}, &Key{SerializedBytes: privateKey}
}

func (v *ecdsaTxSignerVerifier) IsVerificationKeyValid(key PublicKey) bool {
	//TODO: Check that it is a valid ECDSA key
	return key != nil && len(key.SerializedBytes) > 0
}

func (v *ecdsaTxSignerVerifier) VerifyTx(verificationKey PublicKey, tx *token.Tx) error {
	return crypto.VerifyMessage(verificationKey.SerializedBytes, signatureData(tx.GetSerialNumbers()), tx.GetSignature())
}

func (v *ecdsaTxSignerVerifier) SignTx(signingKey PrivateKey, inputs []SerialNumber) (Signature, error) {
	return crypto.SignMessage(signingKey.SerializedBytes, signatureData(inputs))
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

type dummyTxSignerVerifier struct{}

func (v *dummyTxSignerVerifier) NewKeys() (PublicKey, PrivateKey) {
	return &Key{}, &Key{}
}

func (v *dummyTxSignerVerifier) IsVerificationKeyValid(key PublicKey) bool {
	return true
}

func (v *dummyTxSignerVerifier) VerifyTx(verificationKey PublicKey, tx *token.Tx) error {
	return nil
}

func (v *dummyTxSignerVerifier) SignTx(signingKey PrivateKey, inputs []SerialNumber) (Signature, error) {
	return []byte{}, nil
}
