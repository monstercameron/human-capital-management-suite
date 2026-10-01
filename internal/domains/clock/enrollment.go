// TCLOCK-002: enroll, credential and revoke clock devices and kiosk
// installations.
//
// An admin issues a single-use, short-lived enrollment code (also
// deliverable as MDM managed app configuration). The device proves
// possession of an Ed25519 key pair by verifying its signature over a
// server-issued challenge; only the public key, challenge and signature are
// verified here — a raw private key never enters this contract. The
// resulting device identity is scoped to tenant, site, profile and
// timezone, and every rotation, suspension, resumption, revocation and
// site reassignment is a revisioned transition over the previous identity.
// A revoked device's queued offline punches never mature into observations;
// ClassifyQueuedPunch turns them into exceptions instead. The package is
// pure: clocks and randomness arrive as parameters, nothing is persisted or
// emitted.
package clock

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrEnrollmentRejected is the TCLOCK-002 seeded-defect sentinel. A test
	// that probes a reused code, an expired code, an unregistered profile
	// class or an unverified key-possession proof must see this error.
	ErrEnrollmentRejected = errors.New("TCLOCK_002_REJECTED")
	// ErrEnrollmentEvidence identifies an invalid enrollment or transition
	// result that cannot be used as a device identity.
	ErrEnrollmentEvidence = errors.New("clock: enrollment evidence is invalid")
)

// EnrollmentRejection is the stable TCLOCK-002 failure shape.
type EnrollmentRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *EnrollmentRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrEnrollmentRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the TCLOCK_002_REJECTED sentinel to errors.Is.
func (r *EnrollmentRejection) Unwrap() error { return ErrEnrollmentRejected }

func enrollReject(field, state, version, reason string) error {
	return &EnrollmentRejection{Field: field, State: state, Version: version, Reason: reason}
}

const enrollmentVersion = "tclock-enrollment/v1"

// EnrollmentCode is the single-use, short-lived enrollment code or QR an
// admin issues out of band. It names the tenant, site, accepted profile
// class and timezone the resulting device identity will be scoped to.
type EnrollmentCode struct {
	CodeRef   string
	Tenant    values.TenantId
	SiteRef   string
	Profile   SourceClass
	Timezone  string
	IssuedAt  time.Time
	ExpiresAt time.Time
	Consumed  bool
}

func (c EnrollmentCode) Validate() error {
	if err := c.Tenant.Validate(); err != nil {
		return enrollReject("tenant", "", enrollmentVersion, "tenant is invalid")
	}
	if strings.TrimSpace(c.CodeRef) == "" {
		return enrollReject("code_ref", "", enrollmentVersion, "enrollment code reference is required")
	}
	if strings.TrimSpace(c.SiteRef) == "" {
		return enrollReject("site_ref", "", enrollmentVersion, "site reference is required")
	}
	if !c.Profile.Valid() {
		return enrollReject("profile", "", enrollmentVersion, "profile class is not declared")
	}
	if !validTimezone(c.Timezone) {
		return enrollReject("timezone", "", enrollmentVersion, "timezone is required")
	}
	if c.IssuedAt.IsZero() || c.ExpiresAt.IsZero() || !c.ExpiresAt.After(c.IssuedAt) {
		return enrollReject("validity", "", enrollmentVersion, "code validity window is incomplete")
	}
	return nil
}

// activeAt reports whether the code may still be consumed at now: it must
// not already be consumed, and now must fall within its validity window.
func (c EnrollmentCode) activeAt(now time.Time) error {
	if c.Consumed {
		return enrollReject("code.consumed", "CONSUMED", enrollmentVersion, "enrollment code is already consumed")
	}
	if now.Before(c.IssuedAt) || !now.Before(c.ExpiresAt) {
		return enrollReject("code.expires_at", "EXPIRED", enrollmentVersion, "enrollment code is expired or not yet valid")
	}
	return nil
}

// KeyPossessionProof is the device's Ed25519 proof of possession over a
// server-issued challenge, verified with crypto/ed25519 from the standard
// library only. The raw private key never enters this contract.
type KeyPossessionProof struct {
	PublicKey ed25519.PublicKey
	Challenge []byte
	Signature []byte
}

func (p KeyPossessionProof) Verify() error {
	if len(p.PublicKey) != ed25519.PublicKeySize {
		return enrollReject("proof.public_key", "INVALID", enrollmentVersion, "public key has the wrong size")
	}
	if len(p.Challenge) == 0 {
		return enrollReject("proof.challenge", "MISSING", enrollmentVersion, "server challenge is required")
	}
	if len(p.Signature) != ed25519.SignatureSize {
		return enrollReject("proof.signature", "INVALID", enrollmentVersion, "signature has the wrong size")
	}
	if !ed25519.Verify(p.PublicKey, p.Challenge, p.Signature) {
		return enrollReject("proof.signature", "MISMATCH", enrollmentVersion, "signature does not verify against the public key and challenge")
	}
	return nil
}

func keyFingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// EnrollmentRequest binds a code to a device's key-possession proof at now,
// gated by the tenant's signed profile registry.
type EnrollmentRequest struct {
	Code      EnrollmentCode
	Proof     KeyPossessionProof
	DeviceRef string
	Registry  ProfileRegistry
	Now       time.Time
}

// DeviceIdentityState is the closed lifecycle of an enrolled device.
type DeviceIdentityState string

const (
	DeviceActive    DeviceIdentityState = "ACTIVE"
	DeviceSuspended DeviceIdentityState = "SUSPENDED"
	DeviceRevoked   DeviceIdentityState = "REVOKED"
)

func (s DeviceIdentityState) Valid() bool {
	switch s {
	case DeviceActive, DeviceSuspended, DeviceRevoked:
		return true
	}
	return false
}

// DeviceIdentity is the resulting revisioned identity, scoped to tenant,
// site, profile and timezone.
type DeviceIdentity struct {
	DeviceRef            string
	Tenant               values.TenantId
	SiteRef              string
	Profile              SourceClass
	Timezone             string
	PublicKeyFingerprint string
	Revision             uint64
	State                DeviceIdentityState
	Reason               string
	EnrolledAt           time.Time
	TransitionedAt       time.Time
	Digest               string
}

func (d DeviceIdentity) digestBody() string {
	return strings.Join([]string{
		d.DeviceRef, string(d.Tenant), d.SiteRef, string(d.Profile), d.Timezone,
		d.PublicKeyFingerprint, fmt.Sprint(d.Revision), string(d.State),
	}, "\x00")
}

// Enroll consumes a single-use code and verifies device key possession,
// gated by the profile registry, and returns revision 1 of the device
// identity.
func Enroll(req EnrollmentRequest) (DeviceIdentity, error) {
	if req.Now.IsZero() {
		return DeviceIdentity{}, enrollReject("now", "", enrollmentVersion, "server clock is required")
	}
	if err := req.Code.Validate(); err != nil {
		return DeviceIdentity{}, err
	}
	if err := req.Code.activeAt(req.Now); err != nil {
		return DeviceIdentity{}, err
	}
	if err := req.Registry.Accepts(req.Code.Profile, ""); err != nil {
		return DeviceIdentity{}, enrollReject("profile", "UNREGISTERED", enrollmentVersion, err.Error())
	}
	if strings.TrimSpace(req.DeviceRef) == "" {
		return DeviceIdentity{}, enrollReject("device_ref", "", enrollmentVersion, "device reference is required")
	}
	if err := req.Proof.Verify(); err != nil {
		return DeviceIdentity{}, err
	}
	at := req.Now.UTC()
	identity := DeviceIdentity{
		DeviceRef: req.DeviceRef, Tenant: req.Code.Tenant, SiteRef: req.Code.SiteRef,
		Profile: req.Code.Profile, Timezone: req.Code.Timezone,
		PublicKeyFingerprint: keyFingerprint(req.Proof.PublicKey),
		Revision:             1, State: DeviceActive,
		EnrolledAt: at, TransitionedAt: at,
	}
	sum := sha256.Sum256([]byte(identity.digestBody()))
	identity.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return identity, nil
}

// DeviceTransitionKind is the closed vocabulary of revisioned changes a
// device identity can undergo after enrollment.
type DeviceTransitionKind string

const (
	TransitionRotateKey    DeviceTransitionKind = "ROTATE_KEY"
	TransitionSuspend      DeviceTransitionKind = "SUSPEND"
	TransitionResume       DeviceTransitionKind = "RESUME"
	TransitionRevoke       DeviceTransitionKind = "REVOKE"
	TransitionReassignSite DeviceTransitionKind = "REASSIGN_SITE"
)

func (k DeviceTransitionKind) Valid() bool {
	switch k {
	case TransitionRotateKey, TransitionSuspend, TransitionResume, TransitionRevoke, TransitionReassignSite:
		return true
	}
	return false
}

// DeviceTransitionRequest carries the previous identity plus what is
// changing. Reason is required for every transition: it is the audit trail
// a revocation, suspension or reassignment must leave.
type DeviceTransitionRequest struct {
	Previous    DeviceIdentity
	Kind        DeviceTransitionKind
	NewProof    *KeyPossessionProof
	NewSiteRef  string
	NewTimezone string
	Reason      string
	Now         time.Time
}

// TransitionDevice applies one revisioned transition over the previous
// device identity. The previous identity is received by value and never
// mutated: the result is a new revision that references it by its own
// prior fields.
func TransitionDevice(req DeviceTransitionRequest) (DeviceIdentity, error) {
	prev := req.Previous
	if prev.Digest == "" || !prev.State.Valid() {
		return DeviceIdentity{}, enrollReject("previous", "", enrollmentVersion, "transition requires a valid enrolled device identity")
	}
	if prev.State == DeviceRevoked {
		return DeviceIdentity{}, enrollReject("previous.state", string(prev.State), enrollmentVersion, "a revoked device cannot be transitioned further")
	}
	if !req.Kind.Valid() {
		return DeviceIdentity{}, enrollReject("kind", string(prev.State), enrollmentVersion, "transition kind is not declared")
	}
	if req.Now.IsZero() {
		return DeviceIdentity{}, enrollReject("now", string(prev.State), enrollmentVersion, "server clock is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return DeviceIdentity{}, enrollReject("reason", string(prev.State), enrollmentVersion, "a transition reason is required")
	}
	next := prev
	next.Revision = prev.Revision + 1
	next.Reason = req.Reason
	switch req.Kind {
	case TransitionRotateKey:
		if req.NewProof == nil {
			return DeviceIdentity{}, enrollReject("new_proof", string(prev.State), enrollmentVersion, "a new key-possession proof is required to rotate")
		}
		if err := req.NewProof.Verify(); err != nil {
			return DeviceIdentity{}, err
		}
		next.PublicKeyFingerprint = keyFingerprint(req.NewProof.PublicKey)
	case TransitionSuspend:
		if prev.State != DeviceActive {
			return DeviceIdentity{}, enrollReject("previous.state", string(prev.State), enrollmentVersion, "only an active device may be suspended")
		}
		next.State = DeviceSuspended
	case TransitionResume:
		if prev.State != DeviceSuspended {
			return DeviceIdentity{}, enrollReject("previous.state", string(prev.State), enrollmentVersion, "only a suspended device may resume")
		}
		next.State = DeviceActive
	case TransitionRevoke:
		next.State = DeviceRevoked
	case TransitionReassignSite:
		if strings.TrimSpace(req.NewSiteRef) == "" {
			return DeviceIdentity{}, enrollReject("new_site_ref", string(prev.State), enrollmentVersion, "reassignment requires a new site reference")
		}
		if !validTimezone(req.NewTimezone) {
			return DeviceIdentity{}, enrollReject("new_timezone", string(prev.State), enrollmentVersion, "reassignment requires the new site's timezone")
		}
		next.SiteRef = req.NewSiteRef
		next.Timezone = req.NewTimezone
	}
	next.TransitionedAt = req.Now.UTC()
	sum := sha256.Sum256([]byte(next.digestBody()))
	next.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return next, nil
}

// ReplayDevice rebuilds a device identity from its enrollment and an
// ordered list of transitions. It must equal folding the same transitions
// incrementally one at a time; TestTodo_TCLOCK_002_Recovery proves that
// equivalence.
func ReplayDevice(enrolled DeviceIdentity, transitions []DeviceTransitionRequest) (DeviceIdentity, error) {
	current := enrolled
	for _, t := range transitions {
		t.Previous = current
		next, err := TransitionDevice(t)
		if err != nil {
			return DeviceIdentity{}, err
		}
		current = next
	}
	return current, nil
}

// PunchDisposition classifies a queued offline punch by the enrolling
// device's state at the instant it is finally considered for acceptance.
type PunchDisposition string

const (
	DispositionAccepted  PunchDisposition = "ACCEPTED"
	DispositionException PunchDisposition = "EXCEPTION_DEVICE_REVOKED"
)

// ClassifyQueuedPunch reports whether a punch buffered before revocation
// may still land as an observation. A revoked device's queued punches
// become exceptions regardless of how long they had been buffered: they
// never mature into an accepted time observation.
func ClassifyQueuedPunch(identity DeviceIdentity) PunchDisposition {
	if identity.State == DeviceRevoked {
		return DispositionException
	}
	return DispositionAccepted
}

// EnrollmentExplanation is the audit-safe summary of a device identity.
type EnrollmentExplanation struct {
	DeviceRef string
	SiteRef   string
	Profile   SourceClass
	Revision  uint64
	State     DeviceIdentityState
	Digest    string
}

// ExplainEnrollment validates and summarizes a device identity for operator
// display.
func ExplainEnrollment(d DeviceIdentity) (EnrollmentExplanation, error) {
	if d.Digest == "" || !d.State.Valid() {
		return EnrollmentExplanation{}, ErrEnrollmentEvidence
	}
	sum := sha256.Sum256([]byte(d.digestBody()))
	if "sha256:"+hex.EncodeToString(sum[:]) != d.Digest {
		return EnrollmentExplanation{}, fmt.Errorf("%w: digest mismatch", ErrEnrollmentEvidence)
	}
	return EnrollmentExplanation{DeviceRef: d.DeviceRef, SiteRef: d.SiteRef, Profile: d.Profile, Revision: d.Revision, State: d.State, Digest: d.Digest}, nil
}
