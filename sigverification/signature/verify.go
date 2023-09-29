package signature

import (
	"strings"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
)

type Message = signature.Message
type Signature = signature.Signature
type PublicKey = signature.PublicKey

var log = logging.New("verifier")

type Scheme = signature.Scheme

const (
	NoScheme Scheme = signature.NoScheme
	Ecdsa           = signature.Ecdsa
	Bls             = signature.Bls
	Eddsa           = signature.Eddsa
)

type VerifierFactory interface {
	NewVerifier(key PublicKey) (TxVerifier, error)
}

type TxVerifier interface {
	//VerifyTx verifies a signature of a transaction as signed by SignTx
	VerifyTx(*protoblocktx.Tx) error
}

var verifierFactories = map[Scheme]VerifierFactory{
	Ecdsa:    &EcdsaVerifierFactory{},
	NoScheme: &dummyVerifierFactory{},
	Bls:      &BLSVerifierFactory{},
	Eddsa:    &EDDSAVerifierFactory{},
}

func GetVerifierFactory(scheme signature.Scheme) (VerifierFactory, error) {
	if factory, ok := verifierFactories[strings.ToUpper(scheme)]; ok {
		return factory, nil
	}
	return nil, errors.New("scheme not supported for verifier")
}

// NewTxVerifier creates a new TX verifier according to the implementation scheme
func NewTxVerifier(scheme Scheme, key []byte) (TxVerifier, error) {
	if factory, ok := verifierFactories[strings.ToUpper(scheme)]; ok {
		return factory.NewVerifier(key)
	} else {
		return nil, errors.New("scheme not supported")
	}
}
