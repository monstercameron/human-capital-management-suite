// TCLOCK-005: identify workers at shared devices by PIN, badge, QR or
// supervisor override, with per-device and per-worker rate limiting.
//
// A PIN is never stored or compared in the clear: HashPIN derives a salted
// HMAC-SHA256 credential (golang.org/x/crypto is not in go.mod, so the
// standard library crypto/sha256 and crypto/hmac are used directly with a
// per-worker salt) and PINCredential.Verify compares in constant time.
// Badge and QR credentials are opaque issued references with their own
// rotation and revocation state. A supervisor override requires the
// supervisor's own credential evidence and a reason, recorded as separate
// evidence from the worker's own punch. EvaluateAttempt enforces a rate
// limit with lockout for any scope (device or worker) the caller checks it
// against; the package assigns no default scope or threshold — those are
// data supplied by the caller. The package is pure: no clock, no storage,
// no network.
package clock

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrIdentificationRejected is the TCLOCK-005 seeded-defect sentinel. A
	// test that probes a wrong PIN, a revoked badge, a missing override
	// reason or a locked-out scope must see this error.
	ErrIdentificationRejected = errors.New("TCLOCK_005_REJECTED")
	// ErrPINCredential identifies a PIN or salt that cannot be hashed into a
	// credential.
	ErrPINCredential = errors.New("clock: pin credential is invalid")
)

// IdentificationRejection is the stable TCLOCK-005 failure shape.
type IdentificationRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *IdentificationRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrIdentificationRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the TCLOCK_005_REJECTED sentinel to errors.Is.
func (r *IdentificationRejection) Unwrap() error { return ErrIdentificationRejected }

func idReject(field, state, version, reason string) error {
	return &IdentificationRejection{Field: field, State: state, Version: version, Reason: reason}
}

const identificationVersion = "tclock-identification/v1"

// AssuranceLevel grades how strongly an identification method proves the
// claimed worker. It is fixed per method, never inferred from history.
type AssuranceLevel string

const (
	AssuranceLow    AssuranceLevel = "LOW"
	AssuranceMedium AssuranceLevel = "MEDIUM"
	AssuranceHigh   AssuranceLevel = "HIGH"
)

// AssuranceLevel returns the fixed assurance grade for m.
func (m IdentificationMethod) AssuranceLevel() AssuranceLevel {
	switch m {
	case MethodPIN:
		return AssuranceLow
	case MethodBadge, MethodQR:
		return AssuranceMedium
	case MethodFace, MethodFingerprint, MethodSupervisorOverride:
		return AssuranceHigh
	}
	return ""
}

// PINCredential is a worker's salted PIN hash: Hash is
// HMAC-SHA256(key=Salt, message=pin), never the raw PIN.
type PINCredential struct {
	WorkerRef string
	Salt      []byte
	Hash      []byte
}

// HashPIN derives a salted PIN credential. The caller supplies Salt (e.g.
// from crypto/rand); an implicit or reused salt is refused so a PIN cannot
// be silently compared across workers.
func HashPIN(workerRef, pin string, salt []byte) (PINCredential, error) {
	if strings.TrimSpace(workerRef) == "" {
		return PINCredential{}, fmt.Errorf("%w: worker reference is required", ErrPINCredential)
	}
	if len(pin) < 4 {
		return PINCredential{}, fmt.Errorf("%w: pin must be at least 4 digits", ErrPINCredential)
	}
	if len(salt) < 16 {
		return PINCredential{}, fmt.Errorf("%w: salt must be at least 16 bytes", ErrPINCredential)
	}
	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte(pin))
	return PINCredential{WorkerRef: workerRef, Salt: append([]byte(nil), salt...), Hash: mac.Sum(nil)}, nil
}

// Verify reports whether pin hashes to this credential's stored digest,
// compared in constant time.
func (c PINCredential) Verify(pin string) bool {
	if len(c.Salt) == 0 || len(c.Hash) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, c.Salt)
	mac.Write([]byte(pin))
	return hmac.Equal(mac.Sum(nil), c.Hash)
}

// BadgeCredential is an issued, rotatable, revocable badge identifier. The
// scanned BadgeID is expected to already be an opaque, normalized reference
// - never a raw magnetic-stripe or proximity payload.
type BadgeCredential struct {
	WorkerRef string
	BadgeID   string
	IssuedAt  time.Time
	RevokedAt time.Time
}

// Active reports whether the badge is usable at now.
func (c BadgeCredential) Active(now time.Time) bool {
	if strings.TrimSpace(c.BadgeID) == "" || c.IssuedAt.IsZero() || now.Before(c.IssuedAt) {
		return false
	}
	return c.RevokedAt.IsZero() || now.Before(c.RevokedAt)
}

// QRCredential is an issued, rotatable, revocable QR key identifier.
type QRCredential struct {
	WorkerRef string
	KeyID     string
	IssuedAt  time.Time
	RevokedAt time.Time
}

// Active reports whether the QR credential is usable at now.
func (c QRCredential) Active(now time.Time) bool {
	if strings.TrimSpace(c.KeyID) == "" || c.IssuedAt.IsZero() || now.Before(c.IssuedAt) {
		return false
	}
	return c.RevokedAt.IsZero() || now.Before(c.RevokedAt)
}

// SupervisorOverride is the supervisor's own credential evidence reference
// plus a reason, stored as evidence separate from the worker's own punch.
// This package does not itself authenticate the supervisor: EvidenceRef
// must already point at a credential verified by capture or trust.
type SupervisorOverride struct {
	SupervisorRef string
	EvidenceRef   string
	Reason        string
	At            time.Time
}

func (o SupervisorOverride) Validate() error {
	if strings.TrimSpace(o.SupervisorRef) == "" {
		return idReject("override.supervisor_ref", "", identificationVersion, "supervisor reference is required")
	}
	if strings.TrimSpace(o.EvidenceRef) == "" {
		return idReject("override.evidence_ref", "", identificationVersion, "supervisor credential evidence is required")
	}
	if strings.TrimSpace(o.Reason) == "" {
		return idReject("override.reason", "", identificationVersion, "override reason is required")
	}
	if o.At.IsZero() {
		return idReject("override.at", "", identificationVersion, "override instant is required")
	}
	return nil
}

// AttemptOutcome is the closed vocabulary of an identification attempt's
// rate-limit verdict.
type AttemptOutcome string

const (
	AttemptAccepted  AttemptOutcome = "ACCEPTED"
	AttemptRejected  AttemptOutcome = "REJECTED"
	AttemptLockedOut AttemptOutcome = "LOCKED_OUT"
)

// RateLimitPolicy is data, never a hard-coded constant: the per-scope
// attempt ceiling, its rolling window and the lockout duration once the
// ceiling is reached.
type RateLimitPolicy struct {
	MaxAttempts     int
	Window          time.Duration
	LockoutDuration time.Duration
}

func (p RateLimitPolicy) Validate() error {
	if p.MaxAttempts <= 0 {
		return idReject("policy.max_attempts", "", identificationVersion, "must be positive")
	}
	if p.Window <= 0 {
		return idReject("policy.window", "", identificationVersion, "must be positive")
	}
	if p.LockoutDuration <= 0 {
		return idReject("policy.lockout_duration", "", identificationVersion, "must be positive")
	}
	return nil
}

// AttemptWindow is one scope's (device or worker) rolling failure record.
// FailuresAt holds the failure instants still inside the policy window as
// of the last evaluation.
type AttemptWindow struct {
	FailuresAt  []time.Time
	LockedUntil time.Time
}

// EvaluateAttempt applies policy to a scope's prior window at now, given
// whether this attempt itself verified. It returns the outcome and the
// next window state without mutating window in place, so device and
// worker scopes can be evaluated independently from the same call site.
func EvaluateAttempt(policy RateLimitPolicy, window AttemptWindow, now time.Time, verified bool) (AttemptOutcome, AttemptWindow, error) {
	if err := policy.Validate(); err != nil {
		return "", AttemptWindow{}, err
	}
	if now.IsZero() {
		return "", AttemptWindow{}, idReject("now", "", identificationVersion, "server clock is required")
	}
	if !window.LockedUntil.IsZero() && now.Before(window.LockedUntil) {
		return AttemptLockedOut, window, nil
	}
	cutoff := now.Add(-policy.Window)
	pruned := make([]time.Time, 0, len(window.FailuresAt)+1)
	for _, f := range window.FailuresAt {
		if f.After(cutoff) {
			pruned = append(pruned, f)
		}
	}
	if verified {
		return AttemptAccepted, AttemptWindow{FailuresAt: pruned}, nil
	}
	pruned = append(pruned, now)
	if len(pruned) >= policy.MaxAttempts {
		return AttemptLockedOut, AttemptWindow{FailuresAt: pruned, LockedUntil: now.Add(policy.LockoutDuration)}, nil
	}
	return AttemptRejected, AttemptWindow{FailuresAt: pruned}, nil
}

// IdentificationEvidence is the auditable outcome of one accepted
// identification attempt.
type IdentificationEvidence struct {
	Method         IdentificationMethod
	AssuranceLevel AssuranceLevel
	WorkerRef      string
	DeviceRef      string
	SupervisorRef  string
	Digest         string
	At             time.Time
}

// IdentificationRequest is the complete side-effect-free identification
// question. DeviceOutcome and WorkerOutcome must already reflect
// EvaluateAttempt against the device and worker scopes; IdentifyWorker
// refuses to authenticate through a locked-out scope.
type IdentificationRequest struct {
	Method        IdentificationMethod
	DeviceRef     string
	WorkerRef     string
	Now           time.Time
	PINCredential PINCredential
	PINEntered    string
	Badge         BadgeCredential
	QR            QRCredential
	Override      SupervisorOverride
	DeviceOutcome AttemptOutcome
	WorkerOutcome AttemptOutcome
}

// IdentifyWorker authenticates one identification attempt at a shared
// device: PIN, badge, QR or supervisor override. It never authenticates
// through a rate-limited lockout, PIN or credential mismatch: those fail
// closed with TCLOCK_005_REJECTED.
func IdentifyWorker(req IdentificationRequest) (IdentificationEvidence, error) {
	if strings.TrimSpace(req.DeviceRef) == "" {
		return IdentificationEvidence{}, idReject("device_ref", "", identificationVersion, "device reference is required")
	}
	if req.Now.IsZero() {
		return IdentificationEvidence{}, idReject("now", "", identificationVersion, "server clock is required")
	}
	if !req.Method.Valid() {
		return IdentificationEvidence{}, idReject("method", "", identificationVersion, "identification method is not declared")
	}
	if req.DeviceOutcome == AttemptLockedOut {
		return IdentificationEvidence{}, idReject("rate_limit.device", "LOCKED_OUT", identificationVersion, "device is locked out")
	}
	if req.WorkerOutcome == AttemptLockedOut {
		return IdentificationEvidence{}, idReject("rate_limit.worker", "LOCKED_OUT", identificationVersion, "worker is locked out")
	}
	var workerRef, supervisorRef string
	switch req.Method {
	case MethodPIN:
		if strings.TrimSpace(req.WorkerRef) == "" {
			return IdentificationEvidence{}, idReject("worker_ref", "", identificationVersion, "claimed worker reference is required")
		}
		if req.PINCredential.WorkerRef != req.WorkerRef {
			return IdentificationEvidence{}, idReject("pin.credential", "MISMATCH", identificationVersion, "pin credential does not belong to the claimed worker")
		}
		if !req.PINCredential.Verify(req.PINEntered) {
			return IdentificationEvidence{}, idReject("pin", "REJECTED", identificationVersion, "pin does not verify")
		}
		workerRef = req.WorkerRef
	case MethodBadge:
		if strings.TrimSpace(req.WorkerRef) == "" || req.Badge.WorkerRef != req.WorkerRef {
			return IdentificationEvidence{}, idReject("badge.worker_ref", "MISMATCH", identificationVersion, "badge does not belong to the claimed worker")
		}
		if !req.Badge.Active(req.Now) {
			return IdentificationEvidence{}, idReject("badge", "INACTIVE", identificationVersion, "badge is not active")
		}
		workerRef = req.WorkerRef
	case MethodQR:
		if strings.TrimSpace(req.WorkerRef) == "" || req.QR.WorkerRef != req.WorkerRef {
			return IdentificationEvidence{}, idReject("qr.worker_ref", "MISMATCH", identificationVersion, "qr credential does not belong to the claimed worker")
		}
		if !req.QR.Active(req.Now) {
			return IdentificationEvidence{}, idReject("qr", "INACTIVE", identificationVersion, "qr credential is not active")
		}
		workerRef = req.WorkerRef
	case MethodSupervisorOverride:
		if strings.TrimSpace(req.WorkerRef) == "" {
			return IdentificationEvidence{}, idReject("worker_ref", "", identificationVersion, "overridden worker reference is required")
		}
		if err := req.Override.Validate(); err != nil {
			return IdentificationEvidence{}, err
		}
		workerRef = req.WorkerRef
		supervisorRef = req.Override.SupervisorRef
	default:
		return IdentificationEvidence{}, idReject("method", "", identificationVersion, "identification method is not authenticated at a shared device")
	}
	at := req.Now.UTC()
	body := strings.Join([]string{string(req.Method), req.DeviceRef, workerRef, supervisorRef, at.Format(time.RFC3339Nano)}, "\x00")
	sum := sha256.Sum256([]byte(body))
	return IdentificationEvidence{
		Method: req.Method, AssuranceLevel: req.Method.AssuranceLevel(), WorkerRef: workerRef,
		DeviceRef: req.DeviceRef, SupervisorRef: supervisorRef, At: at,
		Digest: "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

// IdentificationExplanation is the audit-safe summary of one identification
// evidence record.
type IdentificationExplanation struct {
	Method         IdentificationMethod
	AssuranceLevel AssuranceLevel
	HasSupervisor  bool
	Digest         string
}

// ExplainIdentification validates and summarizes identification evidence
// for operator display.
func ExplainIdentification(e IdentificationEvidence) (IdentificationExplanation, error) {
	if e.Digest == "" || e.WorkerRef == "" || e.DeviceRef == "" {
		return IdentificationExplanation{}, fmt.Errorf("%w: identification evidence is incomplete", ErrIdentificationRejected)
	}
	return IdentificationExplanation{Method: e.Method, AssuranceLevel: e.AssuranceLevel, HasSupervisor: e.SupervisorRef != "", Digest: e.Digest}, nil
}
