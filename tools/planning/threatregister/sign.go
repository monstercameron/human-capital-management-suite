package threatregister

import (
	"crypto/ed25519"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

// SignRegister signs r's CanonicalDigest with priv and returns r with
// Signature populated. It reuses gateevidence.SignDigest rather than
// reimplementing Ed25519 signing, exactly as
// tools/planning/pilotprovider.SignTopology and
// tools/planning/pilotjurisdiction.SignProfile do.
func SignRegister(r Register, priv ed25519.PrivateKey, publicKeyHex, keyFixture string) (Register, error) {
	digest, err := r.CanonicalDigest()
	if err != nil {
		return r, err
	}
	sigHex, err := gateevidence.SignDigest(priv, digest)
	if err != nil {
		return r, fmt.Errorf("sign digest: %w", err)
	}
	r.Signature = &Signature{
		Algorithm:  "ed25519",
		PublicKey:  publicKeyHex,
		Value:      sigHex,
		KeyFixture: keyFixture,
	}
	return r, nil
}

// VerifyRegisterSignature recomputes r's CanonicalDigest and checks it
// against r.Signature, reusing gateevidence.VerifyDigestSignature. It
// returns a non-nil error for any structural problem (missing signature,
// bad hex, wrong key size) and (false, nil) for a well-formed but invalid
// signature.
func VerifyRegisterSignature(r Register) (bool, error) {
	if r.Signature == nil {
		return false, fmt.Errorf("register has no signature")
	}
	if r.Signature.Algorithm != "ed25519" {
		return false, fmt.Errorf("unsupported signature algorithm %q", r.Signature.Algorithm)
	}
	digest, err := r.CanonicalDigest()
	if err != nil {
		return false, err
	}
	return gateevidence.VerifyDigestSignature(r.Signature.PublicKey, digest, r.Signature.Value)
}
