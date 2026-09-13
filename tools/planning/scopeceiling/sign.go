package scopeceiling

import (
	"crypto/ed25519"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

// SignManifest signs m's CanonicalDigest with priv and returns m with
// Signature populated. It reuses gateevidence.SignDigest rather than
// reimplementing Ed25519 signing.
func SignManifest(m ScopeCeilingManifest, priv ed25519.PrivateKey, publicKeyHex, keyFixture string) (ScopeCeilingManifest, error) {
	digest, err := m.CanonicalDigest()
	if err != nil {
		return m, err
	}
	sigHex, err := gateevidence.SignDigest(priv, digest)
	if err != nil {
		return m, fmt.Errorf("sign digest: %w", err)
	}
	m.Signature = &Signature{
		Algorithm:  "ed25519",
		PublicKey:  publicKeyHex,
		Value:      sigHex,
		KeyFixture: keyFixture,
	}
	return m, nil
}

// VerifyManifestSignature recomputes m's CanonicalDigest and checks it
// against m.Signature, reusing gateevidence.VerifyDigestSignature rather
// than reimplementing Ed25519 verification. It returns a non-nil error for
// any structural problem (missing signature, bad hex, wrong key size) and
// (false, nil) for a well-formed but invalid signature.
func VerifyManifestSignature(m ScopeCeilingManifest) (bool, error) {
	if m.Signature == nil {
		return false, fmt.Errorf("manifest has no signature")
	}
	if m.Signature.Algorithm != "ed25519" {
		return false, fmt.Errorf("unsupported signature algorithm %q", m.Signature.Algorithm)
	}
	digest, err := m.CanonicalDigest()
	if err != nil {
		return false, err
	}
	return gateevidence.VerifyDigestSignature(m.Signature.PublicKey, digest, m.Signature.Value)
}
