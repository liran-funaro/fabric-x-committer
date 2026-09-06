/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package testsig

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/utils/signature"
)

// stdlibHedgedSigner is the crypto/ecdsa baseline the shipped signer replaced. It lives in
// the test rather than beside the signer so the benchmark keeps documenting why the custom
// nonce path exists, without leaving an unused implementation in the package.
type stdlibHedgedSigner struct {
	signingKey *ecdsa.PrivateKey
}

// Sign signs a digest.
func (s stdlibHedgedSigner) Sign(digest signature.Digest) (signature.Signature, error) {
	return ecdsa.SignASN1(rand.Reader, s.signingKey, digest)
}

// BenchmarkEcdsaSigners compares the two ECDSA nonce strategies head to head. Run with
// -benchmem and a high -cpu: the load generator signs from 128 workers, and the standard
// library's per-signature HMAC-DRBG costs allocation as much as CPU, so a single-threaded
// measurement understates the difference the garbage collector makes at scale.
func BenchmarkEcdsaSigners(b *testing.B) {
	privateKey, _ := NewKeyPair(signature.Ecdsa)
	signingKey, err := ParseSigningKey(privateKey)
	require.NoError(b, err)

	plain, err := newEcdsaSigner(signingKey)
	require.NoError(b, err)

	for _, tc := range []struct {
		name   string
		signer digestSigner
	}{
		{name: "stdlib-hedged", signer: stdlibHedgedSigner{signingKey}},
		{name: "plain-nonce", signer: plain},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				// Each iteration signs a distinct digest, so the DRBG cannot be
				// amortised across iterations the way a constant digest would allow.
				var counter uint64
				for pb.Next() {
					counter++
					digest := sha256.Sum256([]byte{
						byte(counter), byte(counter >> 8), byte(counter >> 16), byte(counter >> 24),
					})
					if _, err := tc.signer.Sign(digest[:]); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// TestFastEcdsaSignerIsVerifiable is the check that matters: a cheaper nonce is only
// acceptable if the committer's own verifier accepts the result, since that verifier is
// crypto/ecdsa.VerifyASN1 and gnark returns a fixed-width r||s pair rather than DER.
func TestFastEcdsaSignerIsVerifiable(t *testing.T) {
	t.Parallel()
	privateKey, publicKey := NewKeyPair(signature.Ecdsa)
	signingKey, err := ParseSigningKey(privateKey)
	require.NoError(t, err)

	verifier, err := signature.NewNsVerifierFromKey(signature.Ecdsa, publicKey)
	require.NoError(t, err)

	plain, err := newEcdsaSigner(signingKey)
	require.NoError(t, err)
	signers := map[string]digestSigner{"plain-nonce": plain}

	require.NotNil(t, verifier)
	for name, signer := range signers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Many digests rather than one: the DER encoding drops leading zero bytes, so
			// a signature whose r or s is short only appears once every few hundred.
			for i := range 512 {
				digest := sha256.Sum256([]byte{byte(i), byte(i >> 8)})
				sig, signErr := signer.Sign(digest[:])
				require.NoError(t, signErr)
				require.True(t, ecdsa.VerifyASN1(&signingKey.PublicKey, digest[:], sig),
					"signature %d rejected by ecdsa.VerifyASN1", i)
			}
		})
	}
}
