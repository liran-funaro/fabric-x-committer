package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"

	"github.com/pkg/errors"
)

func NewECDSAKey() (*ecdsa.PrivateKey, error) {
	curve := elliptic.P256()

	// create ecdsa private key
	return ecdsa.GenerateKey(curve, rand.Reader)
}

var verifyError = errors.New("failed to verify signature")

func VerifyMessage(verificationKey *ecdsa.PublicKey, message []byte, signature []byte) error {
	hash := sha256.Sum256(message)

	valid := ecdsa.VerifyASN1(verificationKey, hash[:], signature)
	if !valid {
		return verifyError
	}
	return nil
}

func ParseVerificationKey(key []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(key)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.Errorf("failed to decode PEM block containing public key, got %v", block)
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse public key")
	}

	return pub.(*ecdsa.PublicKey), nil
}

func SerializeVerificationKey(key *ecdsa.PublicKey) ([]byte, error) {
	x509encodedPub, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: x509encodedPub,
	}), nil
}

func ParseSigningKey(keyContent []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(keyContent)
	if block == nil {
		return nil, errors.New("nil block")
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key.(*ecdsa.PrivateKey), nil
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, errors.Errorf("unknown block type: %s", block.Type)
	}
}

func SerializeSigningKey(key *ecdsa.PrivateKey) ([]byte, error) {
	x509encodedPri, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, errors.Wrap(err, "cannot serialize private key")
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: x509encodedPri,
	}), nil

}

func SignMessage(privateKey *ecdsa.PrivateKey, message []byte) ([]byte, error) {
	hash := sha256.Sum256(message)

	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		return nil, err
	}

	return sig, nil
}
