package sigverification_test

import (
	"crypto/ecdsa"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/crypto"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("signtest")

type PrivateKey = []byte

type SignerFactory interface {
	signature.VerifierFactory
	NewKeys() (PrivateKey, signature.PublicKey)
	NewSigner(key PrivateKey) (TxSigner, error)
}

type TxSigner interface {
	//SignTx signs a message and returns the signature
	SignTx([]token.SerialNumber, []token.TxOutput) (signature.Signature, error)
}

var cryptoFactories = map[signature.Scheme]SignerFactory{
	signature.Ecdsa:    &ecdsaSignerFactory{},
	signature.NoScheme: &dummySignerFactory{},
}

func GetSignatureFactory(scheme signature.Scheme) SignerFactory {
	factory, ok := cryptoFactories[strings.ToUpper(scheme)]
	if !ok {
		panic("scheme not supported")
	}
	return factory
}

// ECDSA

type ecdsaSignerFactory struct {
	signature.EcdsaVerifierFactory
}

func (f *ecdsaSignerFactory) NewKeys() (PrivateKey, signature.PublicKey) {
	privateKey, err := crypto.NewECDSAKey()
	if err != nil {
		return nil, nil
	}
	serializedPrivateKey, err := crypto.SerializeSigningKey(privateKey)
	if err != nil {
		return nil, nil
	}
	serializedPublicKey, err := crypto.SerializeVerificationKey(&privateKey.PublicKey)
	if err != nil {
		return nil, nil
	}
	return serializedPrivateKey, serializedPublicKey
}

func (f *ecdsaSignerFactory) NewSigner(key PrivateKey) (TxSigner, error) {
	signingKey, err := crypto.ParseSigningKey(key)
	if err != nil {
		return nil, err
	}
	return &ecdsaTxSigner{signingKey: signingKey}, nil
}

type ecdsaTxSigner struct {
	signingKey *ecdsa.PrivateKey
}

func (s *ecdsaTxSigner) SignTx(inputs []token.SerialNumber, outputs []token.TxOutput) (signature.Signature, error) {
	return crypto.SignMessage(s.signingKey, signature.SignatureData(inputs, outputs))
}

// Dummy

type dummySignerFactory struct {
	signature.EcdsaVerifierFactory
}

func (f *dummySignerFactory) NewKeys() (PrivateKey, signature.PublicKey) {
	return []byte{}, []byte{}
}

func (f *dummySignerFactory) NewSigner(key PrivateKey) (TxSigner, error) {
	return &dummyTxSigner{}, nil
}

type dummyTxSigner struct {
}

func (s *dummyTxSigner) SignTx([]token.SerialNumber, []token.TxOutput) (signature.Signature, error) {
	return []byte{}, nil
}

func ReadOrGenerateKeys(profile signature.Profile) (PrivateKey, signature.PublicKey, error) {
	// Read keys
	if profile.KeyPath != nil && utils.FileExists(profile.KeyPath.VerificationKey) && utils.FileExists(profile.KeyPath.SigningKey) {
		logger.Infof("Verification/signing keys found in files %s/%s. Importing...", profile.KeyPath.VerificationKey, profile.KeyPath.SigningKey)
		verificationKey, err := os.ReadFile(profile.KeyPath.VerificationKey)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "could not read public key from %s", profile.KeyPath.VerificationKey)
		}
		signingKey, err := os.ReadFile(profile.KeyPath.SigningKey)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "could not read private key from %s", profile.KeyPath.SigningKey)
		}
		logger.Infoln("Keys successfully imported!")
		return signingKey, verificationKey, nil
	}

	// Read private key and certificate
	if profile.KeyPath != nil && utils.FileExists(profile.KeyPath.SignCertificate) && utils.FileExists(profile.KeyPath.SigningKey) {
		logger.Infof("Sign cert and key found in files %s/%s. Importing...", profile.KeyPath.SignCertificate, profile.KeyPath.SigningKey)
		verificationKey, err := signature.GetSerializedKeyFromCert(profile.KeyPath.SignCertificate)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "could not read sign cert from %s", profile.KeyPath.SignCertificate)
		}
		signingKey, err := os.ReadFile(profile.KeyPath.SigningKey)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "could not read private key from %s", profile.KeyPath.SigningKey)
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
			return nil, nil, errors.Wrapf(err, "could not write public key into %s.", profile.KeyPath.VerificationKey)
		}
		err = utils.WriteFile(profile.KeyPath.SigningKey, signingKey)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "could not write private key into %s", profile.KeyPath.SigningKey)
		}
		logger.Infoln("Keys successfully exported!")
	}
	return signingKey, verificationKey, nil
}
