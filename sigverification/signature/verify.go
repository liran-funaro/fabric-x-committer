package signature

import (
	"crypto/sha256"
	"encoding/asn1"
	"flag"
	"strings"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

type Message = []byte
type Signature = []byte
type PublicKey = []byte

var log = logging.New("verifier")

type Scheme = string

const (
	NoScheme Scheme = "NONE"
	Ecdsa           = "ECDSA"
	Bls             = "BLS"
	Eddsa           = "EDDSA"
)

var schemeMap = map[string]Scheme{
	"ECDSA": Ecdsa,
	"NONE":  NoScheme,
	"BLS":   Bls,
	"EDDSA": Eddsa,
}

func SchemeVar(p *Scheme, name string, defaultValue Scheme, usage string) {
	*p = defaultValue
	flag.Func(name, usage, func(input string) error {
		if scheme, ok := schemeMap[strings.ToUpper(input)]; ok {
			*p = scheme
			return nil
		}
		return errors.New("scheme not found")
	})
}

type VerifierFactory interface {
	NewVerifier(key PublicKey) (TxVerifier, error)
}

type TxVerifier interface {
	//VerifyTx verifies a signature of a transaction as signed by SignTx
	VerifyTx(*token.Tx) error
}

var verifierFactories = map[Scheme]VerifierFactory{
	Ecdsa:    &EcdsaVerifierFactory{},
	NoScheme: &dummyVerifierFactory{},
	Bls:      &BLSVerifierFactory{},
	Eddsa:    &EDDSAVerifierFactory{},
}

// NewTxVerifier creates a new TX verifier according to the implementation scheme
func NewTxVerifier(scheme Scheme, key []byte) (TxVerifier, error) {
	if factory, ok := verifierFactories[strings.ToUpper(scheme)]; ok {
		return factory.NewVerifier(key)
	} else {
		return nil, errors.New("scheme not supported")
	}
}

func SignatureData(inputs []token.SerialNumber, outputs []token.TxOutput) Message {
	marshaledInputs, err := asn1.Marshal(inputs)
	if err != nil {
		log.Error("failed to serialize the inputs")
		return []byte{}
	}
	marshaledOutputs, err := asn1.Marshal(outputs)
	if err != nil {
		log.Error("failed to serialize the outputs")
		return []byte{}
	}
	h := sha256.New()
	h.Reset()
	h.Write(marshaledInputs)
	hashedInputs := h.Sum(nil)
	h.Reset()
	h.Write(marshaledOutputs)
	hashedOutputs := h.Sum(nil)
	return append(hashedInputs, hashedOutputs...)
}
