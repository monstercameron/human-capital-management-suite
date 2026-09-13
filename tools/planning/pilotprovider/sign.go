package pilotprovider

import (
	"crypto/ed25519"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

// SignTopology signs t's CanonicalDigest with priv and returns t with
// Signature populated. It reuses gateevidence.SignDigest rather than
// reimplementing Ed25519 signing, exactly as
// tools/planning/pilotjurisdiction.SignProfile and
// tools/planning/scopeceiling.SignManifest do.
func SignTopology(t ProviderTopology, priv ed25519.PrivateKey, publicKeyHex, keyFixture string) (ProviderTopology, error) {
	digest, err := t.CanonicalDigest()
	if err != nil {
		return t, err
	}
	sigHex, err := gateevidence.SignDigest(priv, digest)
	if err != nil {
		return t, fmt.Errorf("sign digest: %w", err)
	}
	t.Signature = &Signature{
		Algorithm:  "ed25519",
		PublicKey:  publicKeyHex,
		Value:      sigHex,
		KeyFixture: keyFixture,
	}
	return t, nil
}

// VerifyTopologySignature recomputes t's CanonicalDigest and checks it
// against t.Signature, reusing gateevidence.VerifyDigestSignature. It
// returns a non-nil error for any structural problem (missing signature,
// bad hex, wrong key size) and (false, nil) for a well-formed but invalid
// signature.
func VerifyTopologySignature(t ProviderTopology) (bool, error) {
	if t.Signature == nil {
		return false, fmt.Errorf("topology has no signature")
	}
	if t.Signature.Algorithm != "ed25519" {
		return false, fmt.Errorf("unsupported signature algorithm %q", t.Signature.Algorithm)
	}
	digest, err := t.CanonicalDigest()
	if err != nil {
		return false, err
	}
	return gateevidence.VerifyDigestSignature(t.Signature.PublicKey, digest, t.Signature.Value)
}
