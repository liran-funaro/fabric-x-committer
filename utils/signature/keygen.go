package signature

import (
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
	"strings"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/crypto"
)

var keyGenFactories = map[Scheme]KeyGenFactory{
	Ecdsa:    &ecdsaKeyFactory{},
	NoScheme: &dummyKeyFactory{},
	Bls:      &blsKeyFactory{},
	Eddsa:    &eddsaKeyFactory{},
}

func GetKeyGenFactory(scheme Scheme) (KeyGenFactory, error) {
	if factory, ok := keyGenFactories[strings.ToUpper(scheme)]; ok {
		return factory, nil
	}
	return nil, errors.New("scheme not supported for keygen")
}

type KeyGenFactory interface {
	NewKeys() (PrivateKey, PublicKey)
}

type dummyKeyFactory struct{}

func (f *dummyKeyFactory) NewKeys() (PrivateKey, PublicKey) {
	return []byte{}, []byte{}
}

type blsKeyFactory struct{}

func (b *blsKeyFactory) NewKeys() (PrivateKey, PublicKey) {
	_, _, _, g2 := bn254.Generators()

	randomBytes := make([]byte, fr.Modulus().BitLen())
	rand.Read(randomBytes)

	sk := big.NewInt(0)
	sk.SetBytes(randomBytes)
	sk.Mod(sk, fr.Modulus())

	var pk bn254.G2Affine
	pk.ScalarMultiplication(&g2, sk)
	pkBytes := pk.Bytes()

	return sk.Bytes(), pkBytes[:]
}

type eddsaKeyFactory struct{}

func (b *eddsaKeyFactory) NewKeys() (PrivateKey, PublicKey) {
	pk, sk, _ := ed25519.GenerateKey(nil)
	return sk, pk
}

type ecdsaKeyFactory struct{}

func (f *ecdsaKeyFactory) NewKeys() (PrivateKey, PublicKey) {
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
