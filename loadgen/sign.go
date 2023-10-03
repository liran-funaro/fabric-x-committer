package loadgen

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/crypto"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"

	"google.golang.org/protobuf/proto"
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
		Sign(hash []byte) Signature
		// Verify returns true of the signature matches the hash.
		Verify(sig Signature, hash []byte) bool
		// GetVerificationKey returns the verification key.
		GetVerificationKey() ([]byte, error)
	}

	// TxSignerVerifier supports signing and verifying a TX, given a hash signer.
	TxSignerVerifier struct {
		MarshalOptions proto.MarshalOptions
		HashSigner     HashSignerVerifier
	}
)

// NewTxSignerVerifier creates a new TxSignerVerifier given a workload profile.
func NewTxSignerVerifier(profile *SignatureProfile) *TxSignerVerifier {
	return &TxSignerVerifier{
		MarshalOptions: proto.MarshalOptions{
			AllowPartial:  false,
			Deterministic: true,
		},
		HashSigner: NewHashSignerVerifier(profile),
	}
}

// Verify verifies a TX signature.
func (e *TxSignerVerifier) Verify(tx *protoblocktx.Tx) bool {
	return e.HashSigner.Verify(tx.Signature, signature.HashTx(tx))
}

// Sign signs a TX.
func (e *TxSignerVerifier) Sign(tx *protoblocktx.Tx) {
	tx.Signature = e.HashSigner.Sign(signature.HashTx(tx))
}

// GetVerificationKey returns the verification key.
func (e *TxSignerVerifier) GetVerificationKey() ([]byte, error) {
	return e.HashSigner.GetVerificationKey()
}

// NewHashSignerVerifier creates a new HashSignerVerifier given a workload profile.
func NewHashSignerVerifier(profile *SignatureProfile) HashSignerVerifier {
	switch profile.Scheme {
	case Ecdsa:
		signer := &ecdsaSignerVerifier{}
		signer.readOrGenerateKeys(profile)
		return signer
	case NoScheme:
		return &dummySignerVerifier{}
	default:
		panic(fmt.Sprintf("scheme '%s' not supported", profile.Scheme))
	}
}

// Dummy

type dummySignerVerifier struct{}

func (*dummySignerVerifier) Verify(Signature, []byte) bool {
	return true
}

func (*dummySignerVerifier) Sign([]byte) Signature {
	return nil
}

func (*dummySignerVerifier) GetVerificationKey() ([]byte, error) {
	return nil, nil
}

// ECDSA

type ecdsaSignerVerifier struct {
	signingKey      *ecdsa.PrivateKey
	verificationKey *ecdsa.PublicKey
}

func (e *ecdsaSignerVerifier) Verify(sig Signature, hash []byte) bool {
	return ecdsa.VerifyASN1(e.verificationKey, hash, sig)
}

func (e *ecdsaSignerVerifier) Sign(hash []byte) Signature {
	sig, err := ecdsa.SignASN1(rand.Reader, e.signingKey, hash)
	Must(err)
	return sig
}

func (e *ecdsaSignerVerifier) GetVerificationKey() ([]byte, error) {
	return crypto.SerializeVerificationKey(e.verificationKey)
}

func (e *ecdsaSignerVerifier) ecdsaNewKeys() {
	logger.Debugf("Generating verification/signing keys.")

	privateKey, err := crypto.NewECDSAKey()
	Must(err)

	e.signingKey = privateKey
	e.verificationKey = &privateKey.PublicKey
}

func ecdsaReadVerificationKeyFromCertIfExists(certPath string) *ecdsa.PublicKey {
	pemContent := ReadFileIfExists(certPath)
	if pemContent == nil {
		return nil
	}

	block, _ := pem.Decode(pemContent)
	cert, err := x509.ParseCertificate(block.Bytes)
	Mustf(err, "cannot parse cert: %s", certPath)

	pubKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || cert.PublicKeyAlgorithm != x509.ECDSA {
		panic(fmt.Sprintf("pubkey not ECDSA: %s", certPath))
	}

	return pubKey
}

func (e *ecdsaSignerVerifier) readKeysIfExists(profile *SignatureProfile) {
	if profile.KeyPath == nil {
		return
	}

	var err error

	rawPrivateKey := ReadFileIfExists(profile.KeyPath.SigningKey)
	if rawPrivateKey == nil {
		return
	}

	e.signingKey, err = crypto.ParseSigningKey(rawPrivateKey)
	Must(err)

	rawPublicKey := ReadFileIfExists(profile.KeyPath.VerificationKey)
	if rawPublicKey != nil {
		e.verificationKey, err = crypto.ParseVerificationKey(rawPublicKey)
		Must(err)
	} else {
		e.verificationKey = ecdsaReadVerificationKeyFromCertIfExists(profile.KeyPath.SignCertificate)
	}
}

func (e *ecdsaSignerVerifier) storeKeys(profile *SignatureProfile) {
	if profile.KeyPath != nil && profile.KeyPath.SigningKey != "" && profile.KeyPath.VerificationKey != "" {
		logger.Debugf("Exporting signing/verification keys to: '%s', '%s'.",
			profile.KeyPath.SigningKey, profile.KeyPath.VerificationKey)

		rawPrivateKey, err := crypto.SerializeSigningKey(e.signingKey)
		Must(err)
		rawPublicKey, err := crypto.SerializeVerificationKey(e.verificationKey)
		Must(err)
		WriteFile(profile.KeyPath.SigningKey, rawPrivateKey)
		WriteFile(profile.KeyPath.VerificationKey, rawPublicKey)
	}
}

func (e *ecdsaSignerVerifier) readOrGenerateKeys(profile *SignatureProfile) {
	e.readKeysIfExists(profile)

	if e.signingKey == nil || e.verificationKey == nil {
		e.ecdsaNewKeys()
		e.storeKeys(profile)
	}
}
