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

func VerifyMessage(verificationKey *ecdsa.PublicKey, message []byte, signature []byte) error {
	hash := sha256.Sum256(message)

	valid := ecdsa.VerifyASN1(verificationKey, hash[:], signature)
	if !valid {
		return errors.New("failed to verify signature")
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

func SignMessage(privateKey *ecdsa.PrivateKey, message []byte) ([]byte, error) {
	hash := sha256.Sum256(message)

	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		return nil, err
	}

	return sig, nil
}
