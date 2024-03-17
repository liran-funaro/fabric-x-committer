package loadgen

import (
	"os"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("load-gen-sign")

type (
	// Scheme is the name of the signature scheme.
	Scheme = string
	// Signature represents a signature.
	Signature = []byte
	// PublicKey is the signature public key.
	PublicKey = []byte
	// PrivateKey is the signature private key.
	PrivateKey = []byte

	// TxSignerVerifier supports signing and verifying a TX, given a hash signer.
	TxSignerVerifier struct {
		HashSigner *HashSignerVerifier
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
	for nsIndex := range tx.GetNamespaces() {
		tx.Signatures = append(tx.Signatures, e.HashSigner.Sign(tx, nsIndex))
	}
}

// Verify verifies a signature on the transaction.
func (e *TxSignerVerifier) Verify(tx *protoblocktx.Tx) bool {
	if len(tx.Signatures) < len(tx.Namespaces) {
		return false
	}

	for nsIndex := range tx.GetNamespaces() {
		if !e.HashSigner.Verify(tx, nsIndex) {
			return false
		}
	}

	return true
}

// GetVerificationKey returns the verification key.
func (e *TxSignerVerifier) GetVerificationKey() []byte {
	return e.HashSigner.GetVerificationKey()
}

// NewHashSignerVerifier creates a new HashSignerVerifier given a workload profile.
func NewHashSignerVerifier(profile *SignatureProfile) *HashSignerVerifier {
	logger.Infof("sig profile: %v", profile)
	factory := sigverification_test.GetSignatureFactory(profile.Scheme)

	var signingKey PrivateKey
	var verificationKey PublicKey
	if profile.KeyPath != nil {
		logger.Infof("Attempting to load keys")
		var err error
		signingKey, verificationKey, err = loadKeys(*profile.KeyPath)
		utils.Must(err)
	} else {
		logger.Infof("Generating new keys")
		signingKey, verificationKey = factory.NewKeys()
	}
	verifier, err := factory.NewVerifier(verificationKey)
	utils.Must(err)
	signer, err := factory.NewSigner(signingKey)
	utils.Must(err)

	return &HashSignerVerifier{
		signer:   signer,
		verifier: verifier,
		pubKey:   verificationKey,
		scheme:   profile.Scheme,
	}
}

func loadKeys(keyPath KeyPath) (PrivateKey, PublicKey, error) {
	if !utils.FileExists(keyPath.SigningKey) {
		return nil, nil, errors.Errorf("signing key file not found in %s", keyPath.SigningKey)
	}
	signingKey, err := os.ReadFile(keyPath.SigningKey)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "could not read private key from %s", keyPath.SigningKey)
	}

	if utils.FileExists(keyPath.VerificationKey) {
		if verificationKey, err := os.ReadFile(keyPath.VerificationKey); err != nil {
			return nil, nil, errors.Wrapf(err, "could not read public key from %s", keyPath.VerificationKey)
		} else {
			logger.Infof("Loaded private key and verification key from files %s and %s.",
				keyPath.SigningKey, keyPath.VerificationKey)
			return signingKey, verificationKey, nil
		}
	}

	if utils.FileExists(keyPath.SignCertificate) {
		if verificationKey, err := signature.GetSerializedKeyFromCert(keyPath.SignCertificate); err != nil {
			return nil, nil, errors.Wrapf(err, "could not read sign cert from %s", keyPath.SignCertificate)
		} else {
			logger.Infof("Sign cert and key found in files %s/%s. Importing...",
				keyPath.SignCertificate, keyPath.SigningKey)
			return signingKey, verificationKey, nil
		}
	}

	return nil, nil, errors.New("could not find verification key or sign certificate")
}

// HashSignerVerifier supports signing and verifying a hash value.
type HashSignerVerifier struct {
	signer   sigverification_test.NsSigner
	verifier signature.NsVerifier
	pubKey   PublicKey
	scheme   Scheme
}

// Sign signs a hash.
func (e *HashSignerVerifier) Sign(tx *protoblocktx.Tx, nsIndex int) Signature {
	sign, err := e.signer.SignNs(tx, nsIndex)
	Must(err)
	return sign
}

// Verify verifies a Signature.
func (e *HashSignerVerifier) Verify(tx *protoblocktx.Tx, nsIndex int) bool {
	if err := e.verifier.VerifyNs(tx, nsIndex); err != nil {
		return false
	}
	return true
}

// GetVerificationKey returns the verification key.
func (e *HashSignerVerifier) GetVerificationKey() PublicKey {
	return e.pubKey
}
