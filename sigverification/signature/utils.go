package signature

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/crypto"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("signature")

func GetSerializedKeyFromCert(certPath string) (PublicKey, error) {
	pemContent, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(pemContent)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse cert")
	}

	if cert.PublicKeyAlgorithm != x509.ECDSA {
		return nil, fmt.Errorf("pubkey not ECDSA")
	}

	pubBytes, err := crypto.SerializeVerificationKey(cert.PublicKey.(*ecdsa.PublicKey))
	if err != nil {
		return nil, errors.Wrap(err, "cannot serialize ecdsa pub key")
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
