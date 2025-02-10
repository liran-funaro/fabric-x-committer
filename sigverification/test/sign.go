package sigverification_test

import (
	"fmt"
	"os"

	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	signature2 "github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
)

var logger = logging.New("signtest")

type PrivateKey = signature2.PrivateKey

type SignerVerifierFactory interface {
	signature.VerifierFactory
	SignerFactory
	signature2.KeyGenFactory
}
type signerVerifierFactory struct {
	signature.VerifierFactory
	SignerFactory
	signature2.KeyGenFactory
}

func getFactory(scheme signature.Scheme) (SignerVerifierFactory, error) {
	v, err := signature.GetVerifierFactory(scheme)
	if err != nil {
		return nil, err
	}

	s, err := GetSignerFactory(scheme)
	if err != nil {
		return nil, err
	}

	k, err := signature2.GetKeyGenFactory(scheme)
	if err != nil {
		return nil, err
	}
	return &signerVerifierFactory{v, s, k}, nil
}

func GetSignatureFactory(scheme signature.Scheme) SignerVerifierFactory {
	if f, err := getFactory(scheme); err != nil {
		panic(err.Error())
	} else {
		return f
	}
}

func ReadOrGenerateKeys(profile signature.Profile) (PrivateKey, signature.PublicKey, error) {
	logger.Infof("Read or generate keys for scheme %s", profile.Scheme)
	// Read keys
	if profile.KeyPath != nil && utils.FileExists(profile.KeyPath.VerificationKey) && utils.FileExists(profile.KeyPath.SigningKey) {
		logger.Infof("Verification/signing keys found in files %s/%s. Importing...", profile.KeyPath.VerificationKey, profile.KeyPath.SigningKey)
		verificationKey, err := os.ReadFile(profile.KeyPath.VerificationKey)
		if err != nil {
			return nil, nil, fmt.Errorf("could not read public key from %s: %w", profile.KeyPath.VerificationKey, err)
		}
		signingKey, err := os.ReadFile(profile.KeyPath.SigningKey)
		if err != nil {
			return nil, nil, fmt.Errorf("could not read private key from %s: %w", profile.KeyPath.SigningKey, err)
		}
		logger.Infoln("Keys successfully imported!")
		return signingKey, verificationKey, nil
	}

	// Read private key and certificate
	if profile.KeyPath != nil && utils.FileExists(profile.KeyPath.SignCertificate) && utils.FileExists(profile.KeyPath.SigningKey) {
		logger.Infof("Sign cert and key found in files %s/%s. Importing...", profile.KeyPath.SignCertificate, profile.KeyPath.SigningKey)
		verificationKey, err := signature.GetSerializedKeyFromCert(profile.KeyPath.SignCertificate)
		if err != nil {
			return nil, nil, fmt.Errorf("could not read sign cert from %s: %w", profile.KeyPath.SignCertificate, err)
		}
		signingKey, err := os.ReadFile(profile.KeyPath.SigningKey)
		if err != nil {
			return nil, nil, fmt.Errorf("could not read private key from %s: %w", profile.KeyPath.SigningKey, err)
		}
		logger.Infoln("Keys successfully imported!")
		return signingKey, verificationKey, nil
	}

	// Generate keys
	logger.Info("No verification/signing keys found. Generating...")
	signingKey, verificationKey := GetSignatureFactory(profile.Scheme).NewKeys()

	// Store generated keys
	if profile.KeyPath != nil && profile.KeyPath.SigningKey != "" && profile.KeyPath.VerificationKey != "" {
		err := utils.WriteFile(profile.KeyPath.VerificationKey, verificationKey)
		if err != nil {
			return nil, nil, fmt.Errorf("could not write public key into %s: %w", profile.KeyPath.VerificationKey, err)
		}
		err = utils.WriteFile(profile.KeyPath.SigningKey, signingKey)
		if err != nil {
			return nil, nil, fmt.Errorf("could not write private key into %s: %w", profile.KeyPath.SigningKey, err)
		}
		logger.Infoln("Keys successfully exported!")
	}
	return signingKey, verificationKey, nil
}
