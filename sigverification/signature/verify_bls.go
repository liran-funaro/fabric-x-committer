package signature

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

var q []bn254.G2Affine

func init() {
	_, _, _, g2 := bn254.Generators()
	q = []bn254.G2Affine{g2}
}

const BLS_HASH_PREFIX = "BLS"

// BLS Factory

type BLSVerifierFactory struct{}

func (f *BLSVerifierFactory) NewVerifier(verificationKey []byte) (NsVerifier, error) {
	var pk bn254.G2Affine
	_, err := pk.SetBytes(verificationKey)
	if err != nil {
		return nil, fmt.Errorf("cannot set G2 from verification key bytes, %v", err)
	}

	return &blsTxVerifier{pk: pk}, nil
}

// BLS Verifier

type blsTxVerifier struct {
	pk bn254.G2Affine
}

func (v *blsTxVerifier) VerifyNs(tx *protoblocktx.Tx, nsIndex int) error {
	digest := HashTxNamespace(tx, nsIndex)

	var sig bn254.G1Affine
	_, err := sig.SetBytes(tx.GetSignatures()[nsIndex])
	if err != nil {
		return fmt.Errorf("cannot set G1 from signature bytes")
	}

	g1h, err := bn254.HashToG1(digest, []byte(BLS_HASH_PREFIX))
	if err != nil {
		return fmt.Errorf("cannot convert hash to G1: %v", err)
	}

	left, err := bn254.Pair([]bn254.G1Affine{sig}, q)
	if err != nil {
		return fmt.Errorf("cannot pair G2 with signature: %v", err)
	}

	right, err := bn254.Pair([]bn254.G1Affine{g1h}, []bn254.G2Affine{v.pk})
	if err != nil {
		return fmt.Errorf("cannot pair public key with digest: %v", err)
	}

	if (&left).Equal(&right) {
		return nil
	}

	return fmt.Errorf("signature mismatch")
}
