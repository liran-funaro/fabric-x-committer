package signature

import (
	"crypto/ed25519"
	"errors"
	"math/big"
	mathRand "math/rand"
	"strings"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
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
	NewKeysWithSeed(int64) (PrivateKey, PublicKey)
}

type dummyKeyFactory struct{}

func (f *dummyKeyFactory) NewKeys() (PrivateKey, PublicKey) {
	return f.NewKeysWithSeed(0)
}

func (f *dummyKeyFactory) NewKeysWithSeed(seed int64) (PrivateKey, PublicKey) {
	return []byte{}, []byte{}
}

type blsKeyFactory struct{}

func (b *blsKeyFactory) NewKeys() (PrivateKey, PublicKey) {
	return b.NewKeysWithSeed(mathRand.Int63())
}

func (b *blsKeyFactory) NewKeysWithSeed(seed int64) (PrivateKey, PublicKey) {
	r := mathRand.New(mathRand.NewSource(seed))
	_, _, _, g2 := bn254.Generators()

	randomBytes := make([]byte, fr.Modulus().BitLen())
	r.Read(randomBytes)

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
	return b.NewKeysWithSeed(mathRand.Int63())
}

func (b *eddsaKeyFactory) NewKeysWithSeed(seed int64) (PrivateKey, PublicKey) {
	r := mathRand.New(mathRand.NewSource(seed))
	pk, sk, _ := ed25519.GenerateKey(r)
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

func (f *ecdsaKeyFactory) NewKeysWithSeed(seed int64) (PrivateKey, PublicKey) {
	logger.Debugf("generating new ecdsa keys using reader")
	privateKey, err := crypto.NewECDSAKeyWithSeed(seed)
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
