package signature

import (
	"encoding/asn1"
	"flag"
	"strings"

	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type Message = []byte
type SerialNumber = []byte
type Signature = []byte
type PublicKey = []byte

var log = logging.New("verifier")

type Scheme = string

const (
	NoScheme Scheme = ""
	Ecdsa           = "Ecdsa"
)

var schemeMap = map[string]Scheme{
	"ECDSA": Ecdsa,
	"NONE":  NoScheme,
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
}

//NewTxVerifier creates a new TX verifier according to the implementation scheme
func NewTxVerifier(scheme Scheme, key []byte) (TxVerifier, error) {
	if factory, ok := verifierFactories[scheme]; ok {
		return factory.NewVerifier(key)
	} else {
		return nil, errors.New("scheme not supported")
	}
}

func SignatureData(inputs []SerialNumber) Message {
	data, err := asn1.Marshal(inputs)
	if err != nil {
		log.Error("failed to serialize the inputs")
		return []byte{}
	}
	return data
}
