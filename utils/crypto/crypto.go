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

func NewECDSAKeys() (publicKey []byte, privateKey []byte, err error) {

	// use secp256r1 (prime256v1)
	curve := elliptic.P256()

	// create ecdsa private key
	pri, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "cannot generate ecdsa key with curve %v", curve)
	}

	// serialize
	x509encodedPri, err := x509.MarshalECPrivateKey(pri)
	if err != nil {
		return nil, nil, errors.Wrap(err, "cannot serialize private key")
	}

	privateKey = pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: x509encodedPri,
	})

	x509encodedPub, err := x509.MarshalPKIXPublicKey(pri.Public())
	if err != nil {
		return nil, nil, err
	}

	publicKey = pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: x509encodedPub,
	})

	return publicKey, privateKey, nil
}

func VerifyMessage(publicKey []byte, message []byte, signature []byte) error {

	// hash
	hash := sha256.Sum256(message)

	block, _ := pem.Decode(publicKey)
	if block == nil || block.Type != "PUBLIC KEY" {
		return errors.Errorf("failed to decode PEM block containing public key, got %v", block)
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return errors.Wrap(err, "cannot parse public key")
	}

	valid := ecdsa.VerifyASN1(pub.(*ecdsa.PublicKey), hash[:], signature)
	if !valid {
		return errors.New("failed to verify signature")
	}
	return nil
}

func SignMessage(privateKey []byte, message []byte) ([]byte, error) {

	// hash
	hash := sha256.Sum256(message)

	// convert key
	block, _ := pem.Decode(privateKey)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, errors.Errorf("failed to decode PEM block containing private key, got %v", block)
	}

	priv, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	// sign

	sig, err := ecdsa.SignASN1(rand.Reader, priv, hash[:])
	if err != nil {
		return nil, err
	}

	return sig, nil
}
