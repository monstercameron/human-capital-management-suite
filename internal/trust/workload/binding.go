package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// ErrMTLSWorkloadMismatch means the presented peer certificate and signed
// workload credential do not prove the same live process identity.
var ErrMTLSWorkloadMismatch = errors.New("workload: mTLS peer and issued workload identity do not match")

// BindMTLSIdentity binds an x509-verified peer certificate to a separately
// issued workload credential. Both proofs must be live at at and agree on
// the process instance, role and cell. The returned identity preserves the
// signed credential's authority and is valid only for the overlap of the two
// proof windows, so existing service authorization can consume it directly.
//
// A certificate-derived Identity from MTLSIdentity.AsWorkloadIdentity is not
// sufficient here: the workload credential must have passed Verifier.Verify.
func BindMTLSIdentity(peer MTLSIdentity, issued Identity, at time.Time) (Identity, error) {
	if !peer.Verified() || !peer.ValidAt(at) || !issued.credentialVerified || !issued.ValidAt(at) {
		return Identity{}, ErrMTLSWorkloadMismatch
	}
	if issued.Subject() != peer.Subject() || issued.Role() != ProcessRole(peer.Service()) || issued.Cell() != peer.Cell() {
		return Identity{}, ErrMTLSWorkloadMismatch
	}
	issuedAt := issued.IssuedAt()
	if peer.IssuedAt().After(issuedAt) {
		issuedAt = peer.IssuedAt()
	}
	expiresAt := issued.ExpiresAt()
	if peer.ExpiresAt().Before(expiresAt) {
		expiresAt = peer.ExpiresAt()
	}
	if !issuedAt.Before(expiresAt) || at.Before(issuedAt) || !at.Before(expiresAt) {
		return Identity{}, ErrMTLSWorkloadMismatch
	}
	digest := sha256.Sum256([]byte(issued.Fingerprint() + "\x00" + peer.Fingerprint()))
	issued.issuedAt = issuedAt
	issued.expiresAt = expiresAt
	issued.fingerprint = hex.EncodeToString(digest[:])
	return issued, nil
}
