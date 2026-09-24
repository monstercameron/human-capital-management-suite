package industrypack

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PACK-005: publish signed effective pack versions.
//
// A pack version activates only from a [SignedPackVersion]: an ed25519
// signature by a trusted publisher key over the exact pack identity, version,
// bundle digest, target scope, effective instant and rollback version, with an
// approval by someone other than the publisher. Activation refuses an unsigned
// or tampered envelope, an unknown key, a target other than the one being
// activated, a stale envelope (signed too long ago, not newer than the active
// version, or naming a rollback version that is not the active one) and an
// unapproved or self-approved one. An accepted activation returns a receipt
// binding the exact target, bundle, effective time and rollback version, and
// persists nothing otherwise.

// SignatureRejectionCode is the stable PACK-005 refusal code.
const SignatureRejectionCode = "PACK_005_REJECTED"

// Activation refusal states.
const (
	SignatureMissing    = "UNSIGNED"
	SignatureInvalid    = "SIGNATURE_INVALID"
	SignatureUntrusted  = "KEY_UNTRUSTED"
	SignatureWrongScope = "WRONG_SCOPE"
	SignatureStale      = "STALE"
	SignatureUnapproved = "UNAPPROVED"
	SignatureMalformed  = "MALFORMED"
)

// ErrActivationRefused is the sentinel every PACK-005 refusal unwraps to.
var ErrActivationRefused = errors.New("industrypack: signed pack activation refused")

// ActivationRefusal names what was refused.
type ActivationRefusal struct {
	Code    string
	Field   string
	State   string
	Version string
	Detail  string
}

func (e *ActivationRefusal) Error() string {
	return fmt.Sprintf("%s: %s %s@%s: %s", e.Code, e.Field, e.State, e.Version, e.Detail)
}

// Unwrap exposes the sentinel.
func (e *ActivationRefusal) Unwrap() error { return ErrActivationRefused }

// PackTarget is the exact scope a version activates in.
type PackTarget struct {
	Tenant string `json:"tenant"`
	Cell   string `json:"cell"`
}

func (t PackTarget) String() string { return t.Tenant + "@" + t.Cell }

// SignedPackVersion is the publication envelope.
type SignedPackVersion struct {
	PackID          string
	Industry        Industry
	Version         int
	BundleDigest    string
	Target          PackTarget
	EffectiveAt     time.Time
	RollbackVersion int
	Publisher       string
	Approver        string
	SignedAt        time.Time
	KeyID           string
	Signature       []byte
}

// SigningPayload is the exact byte string the publisher signs.
func (s SignedPackVersion) SigningPayload() []byte {
	return []byte(strings.Join([]string{"hcmnext.industrypack.SignedPackVersion/v1", s.PackID, string(s.Industry), strconv.Itoa(s.Version), s.BundleDigest,
		s.Target.Tenant, s.Target.Cell, s.EffectiveAt.UTC().Format(time.RFC3339Nano), strconv.Itoa(s.RollbackVersion),
		s.Publisher, s.Approver, s.SignedAt.UTC().Format(time.RFC3339Nano), s.KeyID}, "\n"))
}

// SignPackVersion signs s with key under keyID.
func SignPackVersion(s SignedPackVersion, keyID string, key ed25519.PrivateKey) SignedPackVersion {
	s.KeyID = keyID
	s.Signature = ed25519.Sign(key, s.SigningPayload())
	return s
}

// ActivationRequest is the input to [ActivateSigned].
type ActivationRequest struct {
	Envelope SignedPackVersion
	Target   PackTarget
	// ActiveVersion is the version active in Target now (0 when none).
	ActiveVersion int
	TrustedKeys   map[string]ed25519.PublicKey
}

// ActivationReceipt binds what activated.
type ActivationReceipt struct {
	PackID          string
	Industry        Industry
	Version         int
	Target          PackTarget
	BundleDigest    string
	EffectiveAt     time.Time
	RollbackVersion int
	KeyID           string
	Digest          string
	Entitlement     IndustryEntitlementBinding
}

// VerifyActivation checks the signed request and resolves entitlement through
// the trusted application authority without producing effects.
func VerifyActivation(ctx context.Context, authority *IndustryEntitlementAuthority, req ActivationRequest) (ActivationReceipt, error) {
	if authority == nil {
		return ActivationReceipt{}, &EntitlementRejection{Code: EntitlementRejectionCode, State: EntitlementSnapshotAbsent}
	}
	now, err := authority.now()
	if err != nil {
		return ActivationReceipt{}, err
	}
	s := req.Envelope
	v := strconv.Itoa(s.Version)
	refuse := func(field, state, format string, args ...any) (ActivationReceipt, error) {
		return ActivationReceipt{}, &ActivationRefusal{Code: SignatureRejectionCode, Field: field, State: state, Version: v, Detail: fmt.Sprintf(format, args...)}
	}
	switch {
	case strings.TrimSpace(s.PackID) == "" || !s.Industry.Valid() || s.Version < 1 || !strings.HasPrefix(s.BundleDigest, "sha256:") || s.EffectiveAt.IsZero() || s.SignedAt.IsZero():
		return refuse("envelope", SignatureMalformed, "pack id, version, bundle digest, effective and signing instants are required")
	case len(s.Signature) == 0 || s.KeyID == "":
		return refuse("signature", SignatureMissing, "the pack version is not signed")
	}
	key, ok := req.TrustedKeys[s.KeyID]
	if !ok || len(key) != ed25519.PublicKeySize {
		return refuse("key_id", SignatureUntrusted, "key %s is not a trusted publisher key", s.KeyID)
	}
	if !ed25519.Verify(key, s.SigningPayload(), s.Signature) {
		return refuse("signature", SignatureInvalid, "the signature does not cover this envelope")
	}
	if s.Target != req.Target {
		return refuse("target", SignatureWrongScope, "signed for %s, activating in %s", s.Target, req.Target)
	}
	switch {
	case s.SignedAt.After(now):
		return refuse("signed_at", SignatureStale, "signed in the future")
	case authority.maxEnvelopeAge > 0 && now.Sub(s.SignedAt) > authority.maxEnvelopeAge:
		return refuse("signed_at", SignatureStale, "signed %s ago, beyond %s", now.Sub(s.SignedAt), authority.maxEnvelopeAge)
	case s.Version <= req.ActiveVersion:
		return refuse("version", SignatureStale, "version %d is not newer than active version %d", s.Version, req.ActiveVersion)
	case s.RollbackVersion != req.ActiveVersion:
		return refuse("rollback_version", SignatureStale, "rollback version %d is not the active version %d", s.RollbackVersion, req.ActiveVersion)
	}
	if strings.TrimSpace(s.Approver) == "" || strings.EqualFold(strings.TrimSpace(s.Approver), strings.TrimSpace(s.Publisher)) {
		return refuse("approver", SignatureUnapproved, "a publication needs an approver other than the publisher")
	}
	binding, err := authority.admit(ctx, req.Target.Tenant, s.Industry)
	if err != nil {
		return ActivationReceipt{}, err
	}
	r := ActivationReceipt{PackID: s.PackID, Industry: s.Industry, Version: s.Version, Target: s.Target, BundleDigest: s.BundleDigest,
		EffectiveAt: s.EffectiveAt.UTC(), RollbackVersion: s.RollbackVersion, KeyID: s.KeyID}
	r.Entitlement = binding
	sum := sha256.Sum256(append(append([]byte("receipt\n"), s.SigningPayload()...), []byte("\nentitlement="+binding.Fingerprint())...))
	r.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return r, nil
}

// SignedActivationStore persists an accepted activation.
type SignedActivationStore interface {
	SaveActivation(ctx context.Context, r ActivationReceipt) (ActivationEffects, error)
}

// ActivateSigned verifies and, only when accepted, persists an activation.
func ActivateSigned(ctx context.Context, store SignedActivationStore, authority *IndustryEntitlementAuthority, req ActivationRequest) (ActivationReceipt, ActivationEffects, error) {
	if store == nil {
		return ActivationReceipt{}, ActivationEffects{}, fmt.Errorf("%w: store is required", ErrActivationRefused)
	}
	r, err := VerifyActivation(ctx, authority, req)
	if err != nil {
		return ActivationReceipt{}, ActivationEffects{}, err
	}
	effects, err := store.SaveActivation(ctx, r)
	if err != nil {
		return ActivationReceipt{}, ActivationEffects{}, err
	}
	return r, effects, nil
}
