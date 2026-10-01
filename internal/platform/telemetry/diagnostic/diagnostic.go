// Package diagnostic owns the governed runtime diagnostic-elevation contract.
// Elevation is a configuration snapshot, not a package-global log switch.
//
// Authority note: this package performs no cryptographic signature
// verification and holds no approval keys. Request.Signature is an opaque
// approval token supplied by the caller. Apply binds a non-empty token into
// the digest and snapshot for tamper evidence but treats presence as
// presence, never as proof. Genuine authorization must happen through a
// caller-provided SignatureAuthority via ApplyWithAuthority before the
// decision is trusted.
package diagnostic

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const schemaVersion = 2

func Version() int { return schemaVersion }

func Explain() string { return "scoped expiring governed diagnostic elevation" }

type Level uint8

const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
	LevelDebug
)

func (l Level) valid() bool { return l <= LevelDebug }

type Request struct {
	Revision     uint64
	Actor        string
	Approver     string
	Purpose      string
	Scope        string
	Level        Level
	VolumeBudget int
	StartsAt     time.Time
	ExpiresAt    time.Time
	Signature    string
}

type Snapshot struct {
	Revision        uint64
	Actor           string
	Approver        string
	Purpose         string
	Scope           string
	Level           Level
	VolumeBudget    int
	StartsAt        time.Time
	ExpiresAt       time.Time
	SignatureDigest string
	Digest          string
}

// Evidence is the deterministic, non-sensitive record bound to every Decide
// outcome. It carries the approval fingerprint, never the raw signature.
type Evidence struct {
	Revision        uint64
	Digest          string
	Scope           string
	Reason          string
	At              time.Time
	StartsAt        time.Time
	ExpiresAt       time.Time
	LevelRequested  Level
	LevelGranted    Level
	SignatureDigest string
}

func (e Evidence) String() string {
	return fmt.Sprintf("diagnostic revision=%d reason=%s scope=%s digest=%s at=%s level_requested=%d level_granted=%d signature=%s",
		e.Revision, e.Reason, e.Scope, e.Digest, e.At.UTC().Format(time.RFC3339Nano), e.LevelRequested, e.LevelGranted, e.SignatureDigest)
}

type Decision struct {
	Allowed  bool
	Reason   string
	Revision uint64
	Digest   string
	Evidence Evidence
}

// DriftReport is the deterministic convergence result of comparing a
// candidate request against the active snapshot. Converged is true only when
// the candidate revision and all bound governance material match.
type DriftReport struct {
	Converged         bool
	Reason            string
	ActiveRevision    uint64
	CandidateRevision uint64
	Digest            string
	DriftFields       []string
}

// SignatureAuthority verifies that a request signature authorizes the request
// using trust material held by the caller. The package ships no
// implementation; a nil authority always fails closed.
type SignatureAuthority interface {
	VerifySignature(request Request) error
}

type Controller struct {
	mu       sync.RWMutex
	snapshot Snapshot
	used     int
}

var (
	ErrInvalidRequest      = errors.New("diagnostic: invalid elevation request")
	ErrStaleRevision       = errors.New("diagnostic: stale configuration revision")
	ErrUnverifiedSignature = errors.New("diagnostic: unverified elevation signature")
)

// StaleRevisionError carries the evidence for a refused stale Apply while
// remaining matchable with errors.Is against ErrStaleRevision.
type StaleRevisionError struct {
	RequestRevision uint64
	ActiveRevision  uint64
	ActiveDigest    string
}

func (e *StaleRevisionError) Error() string {
	return fmt.Sprintf("diagnostic: stale configuration revision %d (active revision %d digest %s)", e.RequestRevision, e.ActiveRevision, e.ActiveDigest)
}

func (e *StaleRevisionError) Unwrap() error { return ErrStaleRevision }

func Validate(request Request) error {
	if request.Revision == 0 || strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.Approver) == "" || request.Actor == request.Approver || strings.TrimSpace(request.Purpose) == "" || !validScope(request.Scope) || !request.Level.valid() || request.VolumeBudget <= 0 || request.StartsAt.IsZero() || request.ExpiresAt.IsZero() || !request.StartsAt.Before(request.ExpiresAt) || request.ExpiresAt.Sub(request.StartsAt) > time.Hour || strings.TrimSpace(request.Signature) == "" {
		return ErrInvalidRequest
	}
	return nil
}

func validScope(scope string) bool {
	return (strings.HasPrefix(scope, "tenant:") || strings.HasPrefix(scope, "correlation:")) && !strings.ContainsAny(scope, " \t\r\n") && !strings.Contains(scope, "*")
}

// SignatureFingerprint is the non-reversible digest of a raw approval token.
// Snapshots, digests and evidence carry only the fingerprint, never the token.
func SignatureFingerprint(signature string) string {
	sum := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(sum[:])
}

func Digest(request Request) string {
	payload := fmt.Sprintf("diagnostic.v%d\x00%d\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s\x00%s\x00%s", schemaVersion, request.Revision, request.Actor, request.Approver, request.Purpose, request.Scope, request.Level, request.VolumeBudget, request.StartsAt.UTC().Format(time.RFC3339Nano), request.ExpiresAt.UTC().Format(time.RFC3339Nano), SignatureFingerprint(request.Signature))
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func NewController() *Controller { return &Controller{} }

func (c *Controller) Apply(request Request) (Snapshot, error) {
	if err := Validate(request); err != nil {
		return Snapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if request.Revision <= c.snapshot.Revision {
		return Snapshot{}, &StaleRevisionError{RequestRevision: request.Revision, ActiveRevision: c.snapshot.Revision, ActiveDigest: c.snapshot.Digest}
	}
	snapshot := Snapshot{Revision: request.Revision, Actor: request.Actor, Approver: request.Approver, Purpose: request.Purpose, Scope: request.Scope, Level: request.Level, VolumeBudget: request.VolumeBudget, StartsAt: request.StartsAt.UTC(), ExpiresAt: request.ExpiresAt.UTC(), SignatureDigest: SignatureFingerprint(request.Signature), Digest: Digest(request)}
	c.snapshot = snapshot
	c.used = 0
	return snapshot, nil
}

// ApplyWithAuthority verifies the request signature against caller-provided
// trust material before applying. A nil authority or a verification failure
// refuses without mutating controller state.
func (c *Controller) ApplyWithAuthority(request Request, authority SignatureAuthority) (Snapshot, error) {
	if authority == nil {
		return Snapshot{}, ErrUnverifiedSignature
	}
	if err := authority.VerifySignature(request); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrUnverifiedSignature, err.Error())
	}
	return c.Apply(request)
}

func (c *Controller) Decide(scope string, level Level, at time.Time) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snapshot
	evidence := Evidence{Revision: s.Revision, Digest: s.Digest, Scope: s.Scope, At: at.UTC(), StartsAt: s.StartsAt, ExpiresAt: s.ExpiresAt, LevelRequested: level, LevelGranted: s.Level, SignatureDigest: s.SignatureDigest}
	decision := Decision{Revision: s.Revision, Digest: s.Digest}
	if s.Revision == 0 || at.Before(s.StartsAt) || !at.Before(s.ExpiresAt) {
		decision.Reason = "EXPIRED_OR_NOT_ACTIVE"
		evidence.Reason = decision.Reason
		decision.Evidence = evidence
		return decision
	}
	if scope != s.Scope {
		decision.Reason = "SCOPE_MISMATCH"
		evidence.Reason = decision.Reason
		decision.Evidence = evidence
		return decision
	}
	if level > s.Level {
		decision.Reason = "LEVEL_NOT_GRANTED"
		evidence.Reason = decision.Reason
		decision.Evidence = evidence
		return decision
	}
	if c.used >= s.VolumeBudget {
		decision.Reason = "VOLUME_BUDGET_EXHAUSTED"
		evidence.Reason = decision.Reason
		decision.Evidence = evidence
		return decision
	}
	c.used++
	decision.Allowed = true
	decision.Reason = "ALLOWED"
	evidence.Reason = decision.Reason
	decision.Evidence = evidence
	return decision
}

// CheckDrift compares a candidate request against the active snapshot using
// only bound governance inputs. It invents no runtime state and reports every
// differing bound field by stable name.
func CheckDrift(active Snapshot, candidate Request) DriftReport {
	report := DriftReport{ActiveRevision: active.Revision, CandidateRevision: candidate.Revision, Digest: active.Digest}
	var fields []string
	if candidate.Revision != active.Revision {
		fields = append(fields, "revision")
	}
	if candidate.Actor != active.Actor {
		fields = append(fields, "actor")
	}
	if candidate.Approver != active.Approver {
		fields = append(fields, "approver")
	}
	if candidate.Purpose != active.Purpose {
		fields = append(fields, "purpose")
	}
	if candidate.Scope != active.Scope {
		fields = append(fields, "scope")
	}
	if candidate.Level != active.Level {
		fields = append(fields, "level")
	}
	if candidate.VolumeBudget != active.VolumeBudget {
		fields = append(fields, "volume_budget")
	}
	if !candidate.StartsAt.UTC().Equal(active.StartsAt.UTC()) {
		fields = append(fields, "starts_at")
	}
	if !candidate.ExpiresAt.UTC().Equal(active.ExpiresAt.UTC()) {
		fields = append(fields, "expires_at")
	}
	if SignatureFingerprint(candidate.Signature) != active.SignatureDigest {
		fields = append(fields, "signature")
	}
	sort.Strings(fields)
	report.DriftFields = fields
	switch {
	case active.Revision == 0:
		report.Reason = "NO_ACTIVE_CONFIGURATION"
	case len(fields) == 0 && Digest(candidate) == active.Digest:
		report.Converged = true
		report.Reason = "CONVERGED"
	default:
		report.Reason = "DRIFT"
	}
	return report
}

// CheckDrift reports convergence of a candidate request against the active
// configuration held by the controller.
func (c *Controller) CheckDrift(candidate Request) DriftReport {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CheckDrift(c.snapshot, candidate)
}

func (s Snapshot) Explain() string {
	return fmt.Sprintf("diagnostic revision=%d scope=%s level=%d expires=%s", s.Revision, s.Scope, s.Level, s.ExpiresAt.UTC().Format(time.RFC3339Nano))
}
