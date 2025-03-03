package workload

import (
	"errors"
	"fmt"
	"os"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature/sigtest"
)

var logger = logging.New("load-gen-sign")

type (
	// TxSignerVerifier supports signing and verifying a TX, given a hash signer.
	TxSignerVerifier struct {
		HashSigners map[string]*HashSignerVerifier
	}

	// HashSignerVerifier supports signing and verifying a hash value.
	HashSignerVerifier struct {
		signer   *sigtest.NsSigner
		verifier *signature.NsVerifier
		pubKey   signature.PublicKey
		scheme   signature.Scheme
	}
)

var defaultPolicy = Policy{
	Scheme: signature.NoScheme,
}

// NewTxSignerVerifier creates a new TxSignerVerifier given a workload profile.
func NewTxSignerVerifier(policy *PolicyProfile) *TxSignerVerifier {
	signers := make(map[string]*HashSignerVerifier)
	// We set default policy to ensure smooth operation even if the user did not specify anything.
	signers["0"] = NewHashSignerVerifier(&defaultPolicy)
	signers[types.MetaNamespaceID] = NewHashSignerVerifier(&defaultPolicy)

	for nsID, p := range policy.NamespacePolicies {
		signers[nsID] = NewHashSignerVerifier(p)
	}
	return &TxSignerVerifier{
		HashSigners: signers,
	}
}

// Sign signs a TX.
func (e *TxSignerVerifier) Sign(tx *protoblocktx.Tx) {
	for nsIndex, ns := range tx.GetNamespaces() {
		signer := e.HashSigners[ns.NsId]
		tx.Signatures = append(tx.Signatures, signer.Sign(tx, nsIndex))
	}
}

// Verify verifies a signature on the transaction.
func (e *TxSignerVerifier) Verify(tx *protoblocktx.Tx) bool {
	if len(tx.Signatures) < len(tx.Namespaces) {
		return false
	}

	for nsIndex, ns := range tx.GetNamespaces() {
		signer := e.HashSigners[ns.NsId]
		if !signer.Verify(tx, nsIndex) {
			return false
		}
	}

	return true
}

// NewHashSignerVerifier creates a new HashSignerVerifier given a workload profile and a seed.
func NewHashSignerVerifier(profile *Policy) *HashSignerVerifier {
	logger.Debugf("sig profile: %v", profile)
	factory := sigtest.NewSignatureFactory(profile.Scheme)

	var signingKey signature.PrivateKey
	var verificationKey signature.PublicKey
	if profile.KeyPath != nil {
		logger.Infof("Attempting to load keys")
		var err error
		signingKey, verificationKey, err = loadKeys(*profile.KeyPath)
		utils.Must(err)
	} else {
		logger.Debugf("Generating new keys")
		signingKey, verificationKey = factory.NewKeysWithSeed(profile.Seed)
	}
	v, err := factory.NewVerifier(verificationKey)
	utils.Must(err)
	signer, err := factory.NewSigner(signingKey)
	utils.Must(err)

	return &HashSignerVerifier{
		signer:   signer,
		verifier: v,
		pubKey:   verificationKey,
		scheme:   profile.Scheme,
	}
}

// Sign signs a hash.
func (e *HashSignerVerifier) Sign(tx *protoblocktx.Tx, nsIndex int) signature.Signature {
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

// GetVerificationPolicy returns the verification policy.
func (e *HashSignerVerifier) GetVerificationPolicy() *protoblocktx.NamespacePolicy {
	return &protoblocktx.NamespacePolicy{
		Scheme:    e.scheme,
		PublicKey: e.pubKey,
	}
}

// GetVerificationKeyAndSigner returns the verification key and the signer.
func (e *HashSignerVerifier) GetVerificationKeyAndSigner() (signature.PublicKey, *sigtest.NsSigner) {
	return e.pubKey, e.signer
}

func loadKeys(keyPath KeyPath) (signature.PrivateKey, signature.PublicKey, error) {
	if !utils.FileExists(keyPath.SigningKey) {
		return nil, nil, fmt.Errorf("signing key file not found in %s", keyPath.SigningKey)
	}
	signingKey, err := os.ReadFile(keyPath.SigningKey)
	if err != nil {
		return nil, nil, fmt.Errorf("could not read private key from %s: %w", keyPath.SigningKey, err)
	}

	if utils.FileExists(keyPath.VerificationKey) {
		if verificationKey, err := os.ReadFile(keyPath.VerificationKey); err != nil {
			return nil, nil, fmt.Errorf("could not read public key from %s: %w", keyPath.VerificationKey, err)
		} else {
			logger.Infof("Loaded private key and verification key from files %s and %s.",
				keyPath.SigningKey, keyPath.VerificationKey)
			return signingKey, verificationKey, nil
		}
	}

	if utils.FileExists(keyPath.SignCertificate) {
		if verificationKey, err := sigtest.GetSerializedKeyFromCert(keyPath.SignCertificate); err != nil {
			return nil, nil, fmt.Errorf("could not read sign cert from %s: %w", keyPath.SignCertificate, err)
		} else {
			logger.Infof("Sign cert and key found in files %s/%s. Importing...",
				keyPath.SignCertificate, keyPath.SigningKey)
			return signingKey, verificationKey, nil
		}
	}

	return nil, nil, errors.New("could not find verification key or sign certificate")
}
