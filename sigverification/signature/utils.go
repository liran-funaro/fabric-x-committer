package signature

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/crypto"
)

func GetSerializedKeyFromCert(certPath string) (PublicKey, error) {
	pemContent, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(pemContent)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cannot parse cert: %w", err)
	}

	if cert.PublicKeyAlgorithm != x509.ECDSA {
		return nil, fmt.Errorf("pubkey not ECDSA")
	}

	pubBytes, err := crypto.SerializeVerificationKey(cert.PublicKey.(*ecdsa.PublicKey))
	if err != nil {
		return nil, fmt.Errorf("cannot serialize ecdsa pub key: %w", err)
	}

	return pubBytes, nil
}

type Profile struct {
	Scheme  Scheme
	KeyPath *KeyPath
}
type KeyPath struct {
	SigningKey      string
	VerificationKey string
	SignCertificate string
}
