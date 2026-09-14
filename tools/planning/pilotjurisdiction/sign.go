package pilotjurisdiction

import (
	"crypto/ed25519"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

// SignProfile signs p's CanonicalDigest with priv and returns p with
// Signature populated. It reuses gateevidence.SignDigest rather than
// reimplementing Ed25519 signing, exactly as
// tools/planning/scopeceiling.SignManifest does for PHASE-001's manifest.
func SignProfile(p JurisdictionProfile, priv ed25519.PrivateKey, publicKeyHex, keyFixture string) (JurisdictionProfile, error) {
	digest, err := p.CanonicalDigest()
	if err != nil {
		return p, err
	}
	sigHex, err := gateevidence.SignDigest(priv, digest)
	if err != nil {
		return p, fmt.Errorf("sign digest: %w", err)
	}
	p.Signature = &Signature{
		Algorithm:  "ed25519",
		PublicKey:  publicKeyHex,
		Value:      sigHex,
		KeyFixture: keyFixture,
	}
	return p, nil
}

// VerifyProfileSignature recomputes p's CanonicalDigest and checks it against
// p.Signature, reusing gateevidence.VerifyDigestSignature. It returns a
// non-nil error for any structural problem (missing signature, bad hex,
// wrong key size) and (false, nil) for a well-formed but invalid signature.
func VerifyProfileSignature(p JurisdictionProfile) (bool, error) {
	if p.Signature == nil {
		return false, fmt.Errorf("profile has no signature")
	}
	if p.Signature.Algorithm != "ed25519" {
		return false, fmt.Errorf("unsupported signature algorithm %q", p.Signature.Algorithm)
	}
	digest, err := p.CanonicalDigest()
	if err != nil {
		return false, err
	}
	return gateevidence.VerifyDigestSignature(p.Signature.PublicKey, digest, p.Signature.Value)
}
