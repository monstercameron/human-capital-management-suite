package pilotblueprint

import (
	"crypto/ed25519"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

// SignBlueprint signs b's CanonicalDigest with priv and returns b with
// Signature populated. It reuses gateevidence.SignDigest rather than
// reimplementing Ed25519 signing, exactly as
// tools/planning/pilotprovider.SignTopology and
// tools/planning/pilotjurisdiction's own signing helper do.
func SignBlueprint(b Blueprint, priv ed25519.PrivateKey, publicKeyHex, keyFixture string) (Blueprint, error) {
	digest, err := b.CanonicalDigest()
	if err != nil {
		return b, err
	}
	sigHex, err := gateevidence.SignDigest(priv, digest)
	if err != nil {
		return b, fmt.Errorf("sign digest: %w", err)
	}
	b.Signature = &Signature{
		Algorithm:  "ed25519",
		PublicKey:  publicKeyHex,
		Value:      sigHex,
		KeyFixture: keyFixture,
	}
	return b, nil
}

// VerifyBlueprintSignature recomputes b's CanonicalDigest and checks it
// against b.Signature, reusing gateevidence.VerifyDigestSignature. It
// returns a non-nil error for any structural problem (missing signature, bad
// hex, wrong key size) and (false, nil) for a well-formed but invalid
// signature.
func VerifyBlueprintSignature(b Blueprint) (bool, error) {
	if b.Signature == nil {
		return false, fmt.Errorf("blueprint has no signature")
	}
	if b.Signature.Algorithm != "ed25519" {
		return false, fmt.Errorf("unsupported signature algorithm %q", b.Signature.Algorithm)
	}
	digest, err := b.CanonicalDigest()
	if err != nil {
		return false, err
	}
	return gateevidence.VerifyDigestSignature(b.Signature.PublicKey, digest, b.Signature.Value)
}
