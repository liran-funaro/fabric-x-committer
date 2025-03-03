package signature

// DummyVerifier always return success.
type DummyVerifier struct{}

// Verify a digest given a signature.
func (*DummyVerifier) Verify(Digest, Signature) error {
	return nil
}
