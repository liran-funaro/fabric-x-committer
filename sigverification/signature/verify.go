package signature

import (
	"strings"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
)

type (
	Message   = signature.Message
	Signature = signature.Signature
	PublicKey = signature.PublicKey
)

var log = logging.New("verifier")

type Scheme = signature.Scheme

const (
	NoScheme Scheme = signature.NoScheme
	Ecdsa           = signature.Ecdsa
	Bls             = signature.Bls
	Eddsa           = signature.Eddsa
)

type VerifierFactory interface {
	NewVerifier(key PublicKey) (NsVerifier, error)
}

type NsVerifier interface {
	// VerifyNs verifies a signature on a given namespace signed by SignNs.
	VerifyNs(t *protoblocktx.Tx, nsIndex int) error
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

// NewNsVerifier creates a new namespace verifier according to the implementation scheme
func NewNsVerifier(scheme Scheme, key []byte) (NsVerifier, error) {
	if factory, ok := verifierFactories[strings.ToUpper(scheme)]; ok {
		return factory.NewVerifier(key)
	} else {
		return nil, errors.New("scheme not supported")
	}
}
