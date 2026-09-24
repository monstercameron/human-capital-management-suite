package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// defaultIdleTimeout and defaultAbsoluteTimeout are the [Manager] defaults
// used when [ManagerConfig] leaves either at zero.
const (
	defaultIdleTimeout     = 30 * time.Minute
	defaultAbsoluteTimeout = 12 * time.Hour
)

// ManagerConfig configures a [Manager].
type ManagerConfig struct {
	// Now supplies the current time. Nil means time.Now.
	Now func() time.Time
	// IdleTimeout and AbsoluteTimeout are the defaults a [CreateSpec] falls
	// back to when it leaves either at zero. Zero here means the package
	// default (30 minutes idle, 12 hours absolute).
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
}

// Manager owns every server-side session record: creation, refresh
// rotation with replay detection, idle/absolute timeout evaluation,
// revocation and the append-only evidence trail. It is safe for concurrent
// use.
type Manager struct {
	mu sync.Mutex

	now                    func() time.Time
	idleTimeoutDefault     time.Duration
	absoluteTimeoutDefault time.Duration

	sessions map[ID]*Record
	// currentHash maps a refresh token's hash to the session it currently
	// rotates. Exactly one entry exists per active session's current token.
	currentHash map[string]ID
	// retiredHash maps every refresh token hash this package has ever
	// issued and since rotated past (for any session, including revoked
	// ones) to the session it belonged to. Presenting one of these again is
	// the replay TRUST-003 must catch.
	retiredHash map[string]ID
	// sessionCurrentHash is the inverse of the one live entry in
	// currentHash for each session, so that revoking a session can find and
	// invalidate its current token without scanning currentHash.
	sessionCurrentHash map[ID]string

	evidence    []Evidence
	evidenceSeq int
}

// NewManager validates cfg and returns an empty, ready [Manager].
func NewManager(cfg ManagerConfig) (*Manager, error) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	absolute := cfg.AbsoluteTimeout
	if absolute <= 0 {
		absolute = defaultAbsoluteTimeout
	}
	if idle > absolute {
		return nil, fmt.Errorf("%w: idle timeout %s exceeds absolute timeout %s", ErrInvalidCreateSpec, idle, absolute)
	}
	return &Manager{
		now:                    now,
		idleTimeoutDefault:     idle,
		absoluteTimeoutDefault: absolute,
		sessions:               make(map[ID]*Record),
		currentHash:            make(map[string]ID),
		retiredHash:            make(map[string]ID),
		sessionCurrentHash:     make(map[ID]string),
	}, nil
}

// CreateSpec is everything [Manager.Create] needs to open a new session.
type CreateSpec struct {
	// ID is an optional server-generated identifier reserved by a caller that
	// needs to bind the session reference into a credential before creating
	// the durable row. Empty keeps the manager-generated identifier behavior.
	ID                   ID
	Tenant               values.TenantId
	Subject              string
	PrincipalFingerprint string
	Assurance            trust.Assurance
	// IdleTimeout and AbsoluteTimeout override the manager's defaults for
	// this session when positive.
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
}

// Create opens a new active session and returns its record plus the raw
// refresh token that currently rotates it. The raw token is returned
// exactly once: only its hash is ever retained.
func (m *Manager) Create(_ context.Context, spec CreateSpec) (Record, RefreshToken, error) {
	if err := spec.Tenant.Validate(); err != nil {
		return Record{}, "", fmt.Errorf("%w: tenant: %v", ErrInvalidCreateSpec, err)
	}
	if spec.Subject == "" {
		return Record{}, "", fmt.Errorf("%w: subject is empty", ErrInvalidCreateSpec)
	}
	if spec.PrincipalFingerprint == "" {
		return Record{}, "", fmt.Errorf("%w: principal fingerprint is empty", ErrInvalidCreateSpec)
	}
	if spec.Assurance == trust.AssuranceUnspecified {
		return Record{}, "", fmt.Errorf("%w: assurance is unspecified", ErrInvalidCreateSpec)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	idle := spec.IdleTimeout
	if idle <= 0 {
		idle = m.idleTimeoutDefault
	}
	absolute := spec.AbsoluteTimeout
	if absolute <= 0 {
		absolute = m.absoluteTimeoutDefault
	}
	if idle > absolute {
		return Record{}, "", fmt.Errorf("%w: idle timeout %s exceeds absolute timeout %s", ErrInvalidCreateSpec, idle, absolute)
	}

	id := spec.ID
	if id == "" {
		var err error
		id, err = newID()
		if err != nil {
			return Record{}, "", err
		}
	} else if !validID(id) {
		return Record{}, "", fmt.Errorf("%w: invalid session id", ErrInvalidCreateSpec)
	}
	raw, hash, err := newOpaqueToken()
	if err != nil {
		return Record{}, "", err
	}

	now := m.now().UTC()
	rec := &Record{
		id:                   id,
		tenant:               spec.Tenant,
		subject:              spec.Subject,
		principalFingerprint: spec.PrincipalFingerprint,
		assurance:            spec.Assurance,
		status:               StatusActive,
		createdAt:            now,
		lastActivityAt:       now,
		idleTimeout:          idle,
		absoluteExpiresAt:    now.Add(absolute),
	}
	m.sessions[id] = rec
	m.currentHash[hash] = id
	m.sessionCurrentHash[id] = hash
	m.recordEvidenceLocked(id, EvidenceCreated, "", now)

	return *rec, RefreshToken(raw), nil
}

func validID(id ID) bool {
	if !strings.HasPrefix(string(id), "sess_") {
		return false
	}
	decoded, err := tokenEncoding.DecodeString(strings.TrimPrefix(string(id), "sess_"))
	return err == nil && len(decoded) == 16
}

// NewID mints an opaque session identifier for authorities that must bind the
// identifier into a signed credential before creating its session row.
func NewID() (ID, error) { return newID() }

// Get returns the current snapshot of a session, first re-evaluating
// whether it has silently crossed an idle or absolute deadline since it was
// last observed.
func (m *Manager) Get(_ context.Context, id ID) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[id]
	if !ok {
		return Record{}, ErrSessionNotFound
	}
	m.applyExpiryLocked(rec)
	return *rec, nil
}

// Touch records activity on a session, extending its idle window, and
// returns the updated record. It fails if the session is not active
// (expired or revoked).
func (m *Manager) Touch(_ context.Context, id ID) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[id]
	if !ok {
		return Record{}, ErrSessionNotFound
	}
	m.applyExpiryLocked(rec)
	if rec.status != StatusActive {
		return *rec, fmt.Errorf("%w: %s", ErrSessionNotActive, rec.status)
	}
	rec.lastActivityAt = m.now().UTC()
	return *rec, nil
}

// Claim is what a caller re-presenting a session asserts about it, for
// [Manager.Validate] to check against the durable record. A zero field
// skips that field's check.
type Claim struct {
	Tenant    values.TenantId
	Assurance trust.Assurance
}

// Validate checks a session is active and, when claim asserts a tenant or
// an assurance level, that the assertion matches the session's recorded
// value exactly. A mismatch in either direction — a claimed tenant switch
// or a claimed assurance the session was never actually authenticated at —
// is denied and audited: this package never silently reconciles a
// resumed session's claim with a value it never independently verified.
// Raising a session's actual assurance is a step-up event, not something
// Validate infers from a caller's say-so.
func (m *Manager) Validate(_ context.Context, id ID, claim Claim) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[id]
	if !ok {
		return Record{}, ErrSessionNotFound
	}
	m.applyExpiryLocked(rec)
	if rec.status != StatusActive {
		return *rec, fmt.Errorf("%w: %s", ErrSessionNotActive, rec.status)
	}
	now := m.now().UTC()
	if claim.Tenant != "" && claim.Tenant != rec.tenant {
		m.recordEvidenceLocked(id, EvidenceDenied, ReasonTenantMismatch, now)
		return *rec, fmt.Errorf("%w: session is %q, claim is %q", ErrTenantMismatch, rec.tenant, claim.Tenant)
	}
	if claim.Assurance != trust.AssuranceUnspecified && claim.Assurance != rec.assurance {
		m.recordEvidenceLocked(id, EvidenceDenied, ReasonAssuranceMismatch, now)
		return *rec, fmt.Errorf("%w: session is %s, claim is %s", ErrAssuranceMismatch, rec.assurance, claim.Assurance)
	}
	rec.lastActivityAt = now
	return *rec, nil
}

// Refresh presents a refresh token, rotating it: the presented token is
// retired and a new one takes its place. Presenting a token that was
// already retired — issued by this manager at some point but no longer the
// session's current token — is a replay: [Manager] revokes the entire
// session immediately and returns [ErrRefreshReplay], regardless of
// whether the replayed token happens to be the immediately previous one or
// an older one further back in the chain.
func (m *Manager) Refresh(_ context.Context, presented RefreshToken) (Record, RefreshToken, error) {
	hash := hashToken(string(presented))

	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()

	if id, seen := m.retiredHash[hash]; seen {
		if rec, ok := m.sessions[id]; ok && rec.status == StatusActive {
			m.revokeLocked(rec, ReasonRefreshReplay, now)
		}
		return Record{}, "", ErrRefreshReplay
	}

	id, ok := m.currentHash[hash]
	if !ok {
		return Record{}, "", ErrRefreshUnknown
	}
	rec := m.sessions[id]
	m.applyExpiryLocked(rec)
	if rec.status != StatusActive {
		return *rec, "", fmt.Errorf("%w: %s", ErrSessionNotActive, rec.status)
	}

	newRaw, newHash, err := newOpaqueToken()
	if err != nil {
		return Record{}, "", err
	}

	delete(m.currentHash, hash)
	m.retiredHash[hash] = id
	m.currentHash[newHash] = id
	m.sessionCurrentHash[id] = newHash
	rec.rotationCount++
	rec.lastActivityAt = now
	m.recordEvidenceLocked(id, EvidenceRotated, "", now)

	return *rec, RefreshToken(newRaw), nil
}

// Revoke transitions a session to StatusRevoked immediately, invalidating
// its current refresh token. reason defaults to [ReasonManualRevoke] when
// empty.
func (m *Manager) Revoke(_ context.Context, id ID, reason string) (Record, error) {
	if reason == "" {
		reason = ReasonManualRevoke
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[id]
	if !ok {
		return Record{}, ErrSessionNotFound
	}
	if rec.status != StatusActive {
		return *rec, fmt.Errorf("%w: %s", ErrSessionNotActive, rec.status)
	}
	m.revokeLocked(rec, reason, m.now().UTC())
	return *rec, nil
}

// Evidence returns a copy of every evidence record for id, oldest first.
func (m *Manager) Evidence(id ID) []Evidence {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Evidence
	for _, e := range m.evidence {
		if e.SessionID == id {
			out = append(out, e)
		}
	}
	return out
}

// applyExpiryLocked mutates rec in place when it has silently crossed an
// idle or absolute deadline since it was last observed, recording evidence
// for the transition exactly once. Callers must hold m.mu.
func (m *Manager) applyExpiryLocked(rec *Record) {
	if rec.status != StatusActive {
		return
	}
	now := m.now().UTC()
	if !now.Before(rec.absoluteExpiresAt) {
		rec.status = StatusExpiredAbsolute
		rec.revokedReason = ReasonAbsoluteTimeout
		m.recordEvidenceLocked(rec.id, EvidenceExpired, ReasonAbsoluteTimeout, now)
		return
	}
	if now.Sub(rec.lastActivityAt) > rec.idleTimeout {
		rec.status = StatusExpiredIdle
		rec.revokedReason = ReasonIdleTimeout
		m.recordEvidenceLocked(rec.id, EvidenceExpired, ReasonIdleTimeout, now)
	}
}

// revokeLocked transitions rec to StatusRevoked. Its current refresh token's
// index entry is deliberately left in place rather than deleted: Refresh
// still finds the session through it and denies with [ErrSessionNotActive],
// which is what lets that lookup distinguish "this exact token is fine but
// the session behind it is over" from [ErrRefreshUnknown] (a token this
// manager never issued) and [ErrRefreshReplay] (a token this session
// already rotated past). Callers must hold m.mu.
func (m *Manager) revokeLocked(rec *Record, reason string, at time.Time) {
	rec.status = StatusRevoked
	rec.revokedReason = reason
	m.recordEvidenceLocked(rec.id, EvidenceRevoked, reason, at)
}

// recordEvidenceLocked appends one evidence entry. Callers must hold m.mu.
func (m *Manager) recordEvidenceLocked(id ID, kind EvidenceKind, reason string, at time.Time) {
	m.evidenceSeq++
	m.evidence = append(m.evidence, Evidence{
		ID:        newEvidenceID(id, kind, reason, at, m.evidenceSeq),
		SessionID: id,
		Kind:      kind,
		Reason:    reason,
		At:        at,
	})
}
