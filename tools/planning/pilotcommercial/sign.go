package pilotcommercial

import (
	"crypto/ed25519"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

// SignFreeze signs f's CanonicalDigest with priv and returns f with
// Signature populated. It reuses gateevidence.SignDigest rather than
// reimplementing Ed25519 signing, exactly as
// tools/planning/pilotprovider.SignTopology,
// tools/planning/pilotjurisdiction.SignProfile and
// tools/planning/scopeceiling.SignManifest do.
func SignFreeze(f PilotCommercialFreeze, priv ed25519.PrivateKey, publicKeyHex, keyFixture string) (PilotCommercialFreeze, error) {
	digest, err := f.CanonicalDigest()
	if err != nil {
		return f, err
	}
	sigHex, err := gateevidence.SignDigest(priv, digest)
	if err != nil {
		return f, fmt.Errorf("sign digest: %w", err)
	}
	f.Signature = &Signature{
		Algorithm:  "ed25519",
		PublicKey:  publicKeyHex,
		Value:      sigHex,
		KeyFixture: keyFixture,
	}
	return f, nil
}

// VerifyFreezeSignature recomputes f's CanonicalDigest and checks it against
// f.Signature, reusing gateevidence.VerifyDigestSignature. It returns a
// non-nil error for any structural problem (missing signature, bad hex,
// wrong key size) and (false, nil) for a well-formed but invalid signature.
func VerifyFreezeSignature(f PilotCommercialFreeze) (bool, error) {
	if f.Signature == nil {
		return false, fmt.Errorf("freeze has no signature")
	}
	if f.Signature.Algorithm != "ed25519" {
		return false, fmt.Errorf("unsupported signature algorithm %q", f.Signature.Algorithm)
	}
	digest, err := f.CanonicalDigest()
	if err != nil {
		return false, err
	}
	return gateevidence.VerifyDigestSignature(f.Signature.PublicKey, digest, f.Signature.Value)
}
