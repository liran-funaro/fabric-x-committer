package sigverification_test

import (
	"crypto/rand"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
)

// BLS

type blsTxSigner struct {
	sk *big.Int
}

func (b *blsTxSigner) SignTx(serialNumbers []token.SerialNumber, txOutputs []token.TxOutput) (signature.Signature, error) {
	h := signature.SignatureData(serialNumbers, txOutputs)

	g1h, err := bn254.HashToG1(h, []byte(signature.BLS_HASH_PREFIX))
	if err != nil {
		panic(err)
	}

	sig := g1h.ScalarMultiplication(&g1h, b.sk).Bytes()
	return sig[:], nil
}

type blsSignerFactory struct {
	signature.BLSVerifierFactory
}

func (b *blsSignerFactory) NewKeys() (PrivateKey, signature.PublicKey) {
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

func (b *blsSignerFactory) NewSigner(key PrivateKey) (TxSigner, error) {
	sk := big.NewInt(0)
	sk.SetBytes(key)

	return &blsTxSigner{sk}, nil
}
