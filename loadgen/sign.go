package loadgen

import (
	"fmt"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("load-gen-sign")

const (
	// NoScheme does not sign message.
	NoScheme Scheme = ""
	// Ecdsa sign messages using ECDSA.
	Ecdsa Scheme = "ECDSA"
)

type (
	// Scheme is the name of the signature scheme.
	Scheme = string
	// Signature represents a signature.
	Signature = []byte
	// PublicKey is the signature public key.
	PublicKey = []byte
	// PrivateKey is the signature private key.
	PrivateKey = []byte

	// HashSignerVerifier supports signing and verifying a hash value.
	HashSignerVerifier interface {
		// Sign signs a hash.
		Sign(*protoblocktx.Tx) signature.Signature
		// Verify verifies a Signature.
		Verify(*protoblocktx.Tx) bool
		// GetVerificationKey returns the verification key.
		GetVerificationKey() []byte
	}

	// TxSignerVerifier supports signing and verifying a TX, given a hash signer.
	TxSignerVerifier struct {
		HashSigner HashSignerVerifier
	}
)

// NewTxSignerVerifier creates a new TxSignerVerifier given a workload profile.
func NewTxSignerVerifier(profile *SignatureProfile) *TxSignerVerifier {
	return &TxSignerVerifier{
		HashSigner: NewHashSignerVerifier(profile),
	}
}

// Sign signs a TX.
func (e *TxSignerVerifier) Sign(tx *protoblocktx.Tx) {
	tx.Signature = e.HashSigner.Sign(tx)
}

// Verify verifies a signature on the transaction.
func (e *TxSignerVerifier) Verify(tx *protoblocktx.Tx) bool {
	return e.HashSigner.Verify(tx)
}

// GetVerificationKey returns the verification key.
func (e *TxSignerVerifier) GetVerificationKey() []byte {
	return e.HashSigner.GetVerificationKey()
}

// NewHashSignerVerifier creates a new HashSignerVerifier given a workload profile.
func NewHashSignerVerifier(profile *SignatureProfile) HashSignerVerifier {
	switch profile.Scheme {
	case Ecdsa:
		factory := sigverification_test.GetSignatureFactory(signature.Ecdsa)
		pvtKey, pubKey := factory.NewKeys()
		txSigner, _ := factory.NewSigner(pvtKey)

		verifier, err := signature.NewTxVerifier(signature.Ecdsa, pubKey)
		Must(err)
		return &ecdsaSignerVerifier{
			txSigner: txSigner,
			pubKey:   pubKey,
			verifier: verifier,
		}
	case NoScheme:
		return &dummySignerVerifier{}
	default:
		panic(fmt.Sprintf("scheme '%s' not supported", profile.Scheme))
	}
}

// Dummy

type dummySignerVerifier struct{}

func (*dummySignerVerifier) Sign(*protoblocktx.Tx) Signature {
	return nil
}

func (*dummySignerVerifier) Verify(*protoblocktx.Tx) bool {
	return true
}

func (*dummySignerVerifier) GetVerificationKey() []byte {
	return nil
}

// ECDSA

type ecdsaSignerVerifier struct {
	txSigner sigverification_test.TxSigner
	pubKey   []byte
	verifier signature.TxVerifier
}

func (e *ecdsaSignerVerifier) Sign(tx *protoblocktx.Tx) Signature {
	sign, err := e.txSigner.SignTx(tx)
	Must(err)
	return sign
}

func (e *ecdsaSignerVerifier) Verify(tx *protoblocktx.Tx) bool {
	if err := e.verifier.VerifyTx(tx); err != nil {
		return false
	}
	return true
}

func (e *ecdsaSignerVerifier) GetVerificationKey() []byte {
	return e.pubKey
}
