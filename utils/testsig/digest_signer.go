/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package testsig

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"math/big"

	"github.com/cockroachdb/errors"
	"github.com/consensys/gnark-crypto/ecc/bn254"
	"golang.org/x/crypto/cryptobyte"
	cryptobyteasn1 "golang.org/x/crypto/cryptobyte/asn1"

	"github.com/hyperledger/fabric-x-committer/utils/signature"
)

type (
	// eddsaSigner signs using the EDDSA scheme.
	eddsaSigner struct {
		PrivateKey ed25519.PrivateKey
	}

	// blsSigner signs using the BLS scheme.
	blsSigner struct {
		sk *big.Int
	}

	// ecdsaSigner signs using the ECDSA scheme with a plain random nonce.
	//
	// It does not use crypto/ecdsa.SignASN1, which signs "hedged" (FIPS 186-5 via
	// draft-irtf-cfrg-det-sigs-with-noise-04): every signature there builds a fresh
	// HMAC-SHA-512 DRBG personalized with the private key and the digest. A CPU profile of
	// the load generator at 193,000 tps put that DRBG at 11.8% of process CPU -- 1.34x the
	// k*G scalar multiplication it exists to feed -- and 23% of everything the process
	// allocated. Measured on 32 cores at GOGC=400, skipping it is worth 2.09x on the signing
	// path (2,942 -> 1,409 ns/op) and 2.6x on allocation (6,064 -> 2,305 B/op).
	//
	// Hedging protects a private key against an RNG failure. This package signs synthetic
	// transactions with throwaway test identities, so it buys nothing here, while the
	// signatures remain ordinary ECDSA that crypto/ecdsa.VerifyASN1 accepts unchanged.
	ecdsaSigner struct {
		curve elliptic.Curve
		order *big.Int
		// The secret scalar, hoisted out of the key once: PrivateKey.D is deprecated, and
		// PrivateKey.Bytes is the supported way to reach it.
		secret *big.Int
	}
)

// Sign signs a digest.
func (b *eddsaSigner) Sign(digest signature.Digest) (signature.Signature, error) {
	sig, err := b.PrivateKey.Sign(nil, digest, &ed25519.Options{
		Context: "Example_ed25519ctx",
	})
	return sig, errors.Wrap(err, "signing failed")
}

// Sign signs a digest.
func (b *blsSigner) Sign(digest signature.Digest) (signature.Signature, error) {
	g1h, err := bn254.HashToG1(digest, []byte(signature.BlsHashPrefix))
	if err != nil {
		return nil, errors.Wrap(err, "signing failed")
	}
	sig := g1h.ScalarMultiplication(&g1h, b.sk).Bytes()
	return sig[:], nil
}

// newEcdsaSigner builds a signer over the given key's curve order.
func newEcdsaSigner(signingKey *ecdsa.PrivateKey) (*ecdsaSigner, error) {
	secret, err := signingKey.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read ECDSA signing key")
	}
	return &ecdsaSigner{
		curve:  signingKey.Curve,
		order:  signingKey.Params().N,
		secret: new(big.Int).SetBytes(secret),
	}, nil
}

// Sign signs a digest.
func (s *ecdsaSigner) Sign(digest signature.Digest) (signature.Signature, error) {
	k, err := s.nonce()
	if err != nil {
		return nil, err
	}

	// r is the x-coordinate of k*G reduced mod n. ScalarBaseMult is deprecated in favour of
	// crypto/ecdh and crypto/ecdsa, neither of which exposes a signer that skips the DRBG,
	// and it is the only public route to the same optimized curve implementation the
	// standard library signs with.
	x, _ := s.curve.ScalarBaseMult(k.Bytes()) //nolint:staticcheck // no public alternative.
	r := x.Mod(x, s.order)
	if r.Sign() == 0 {
		return s.Sign(digest)
	}

	// sig = k^-1 * (e + r*d) mod n.
	sig := new(big.Int).Mul(r, s.secret)
	sig.Add(sig, hashToInt(digest, s.order))
	sig.Mul(sig, new(big.Int).ModInverse(k, s.order))
	sig.Mod(sig, s.order)
	if sig.Sign() == 0 {
		return s.Sign(digest)
	}

	der, err := marshalSignatureDER(r, sig)
	return der, err
}

// nonce returns a uniform scalar in [1, n-1] per FIPS 186-4 Appendix B.5.1.
func (s *ecdsaSigner) nonce() (*big.Int, error) {
	// 64 bits wider than the order, so the modular reduction's bias is negligible. That is
	// why B.5.1 asks for an over-wide buffer rather than rejection sampling.
	buf := make([]byte, (s.order.BitLen()+7)/8+8)
	if _, err := rand.Read(buf); err != nil {
		return nil, errors.Wrap(err, "failed to read nonce entropy")
	}
	k := new(big.Int).SetBytes(buf)
	k.Mod(k, new(big.Int).Sub(s.order, big.NewInt(1)))
	return k.Add(k, big.NewInt(1)), nil
}

// hashToInt converts a digest to an integer as SEC 1 section 4.1.3 step 5 specifies.
func hashToInt(digest signature.Digest, order *big.Int) *big.Int {
	orderBytes := (order.BitLen() + 7) / 8
	if len(digest) > orderBytes {
		digest = digest[:orderBytes]
	}
	e := new(big.Int).SetBytes(digest)
	if excess := len(digest)*8 - order.BitLen(); excess > 0 {
		e.Rsh(e, uint(excess))
	}
	return e
}

// marshalSignatureDER encodes (r, s) as the DER SEQUENCE crypto/ecdsa.VerifyASN1 expects.
//
// cryptobyte rather than encoding/asn1, which is reflection driven: a CPU profile of the load
// generator at 451,826 tps put encoding/asn1.Marshal at 14.31% of process CPU, against 21.79%
// for the elliptic curve multiplication in the same signature. This is also what
// crypto/ecdsa.SignASN1 uses, so the output is identical byte for byte, including the leading
// zero DER requires when an integer's high bit is set.
func marshalSignatureDER(r, s *big.Int) ([]byte, error) {
	var b cryptobyte.Builder
	b.AddASN1(cryptobyteasn1.SEQUENCE, func(seq *cryptobyte.Builder) {
		seq.AddASN1BigInt(r)
		seq.AddASN1BigInt(s)
	})
	der, err := b.Bytes()
	return der, errors.Wrap(err, "failed to encode signature")
}
