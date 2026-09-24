package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// StoreRecord is the durable-storage shape of a [Record]. It exists because
// Record's own fields are unexported (by design: [Manager] is the only
// producer, and every accessor returns a copy) and an implementation of
// [Store] living outside this package -- internal/trust/session/pgstore, in
// particular -- has no other way to construct or inspect one. StoreRecord
// carries the same information plus the two fields a durable backing needs
// that the in-memory [Manager] tracks implicitly in its own maps:
// CurrentTokenHash (the hash [Manager.currentHash] indexes) and Generation
// (how many times [Manager.Refresh] has rotated this session, matching
// Record's own RotationCount but named for what a durable [Store] indexes
// its replay table by), plus Version, the compare-and-swap fence a durable
// store needs and an in-process mutex does not.
type StoreRecord struct {
	ID                   ID
	Tenant               values.TenantId
	Subject              string
	PrincipalFingerprint string
	Assurance            trust.Assurance
	Status               Status
	RevokedReason        string
	CreatedAt            time.Time
	LastActivityAt       time.Time
	IdleTimeout          time.Duration
	AbsoluteExpiresAt    time.Time
	RotationCount        int
	CurrentTokenHash     string
	Generation           int64
	Version              int64
}

// Record returns the [Record] view of sr, exactly as [Manager] would hand
// back from Create/Get/Touch/Validate/Refresh/Revoke: a value, never a
// pointer into anything mutable.
func (sr StoreRecord) Record() Record {
	return Record{
		id:                   sr.ID,
		tenant:               sr.Tenant,
		subject:              sr.Subject,
		principalFingerprint: sr.PrincipalFingerprint,
		assurance:            sr.Assurance,
		status:               sr.Status,
		revokedReason:        sr.RevokedReason,
		createdAt:            sr.CreatedAt,
		lastActivityAt:       sr.LastActivityAt,
		idleTimeout:          sr.IdleTimeout,
		absoluteExpiresAt:    sr.AbsoluteExpiresAt,
		rotationCount:        sr.RotationCount,
	}
}

// Store is the durable persistence port [PersistentManager] delegates to.
// It is deliberately shaped after [Manager]'s own method set (see doc.go's
// "What this package does not do": a durable store is "its own qualified
// adapter, not a detail this todo may invent" -- this is that adapter's
// port) rather than after a generic CRUD/repository shape, so that every
// safety property TRUST-003 already proved for the in-memory
// implementation -- single-use refresh rotation, family-wide revoke on
// replay, idle/absolute timeout evaluation, an append-only evidence trail
// -- has exactly one durable counterpart to satisfy, not a translation
// layer's worth of impedance mismatch.
//
// Every method is expected to be one atomic operation: a [Store]
// implementation over a SQL database runs each in its own transaction
// (SECARCH-002's own GREEN clause: create/rotate/revoke/replay-detect must
// survive a restart and be visible to every replica within a bounded
// propagation window), never partially applied. [EvaluateExpiry] is the
// shared, pure decision every read-path method (Get, Touch, Revoke,
// Refresh) applies to a freshly loaded row before deciding whether that
// read itself also durably persists an idle/absolute expiry transition --
// exactly mirroring what [Manager.applyExpiryLocked] does for the in-memory
// path, so the two implementations can never quietly disagree about when a
// session expires.
type Store interface {
	// Create durably persists rec as a brand new session at rec.Generation
	// (always 0 for a session [PersistentManager.Create] mints) and its
	// CREATED evidence, atomically. rec.ID must not already exist.
	Create(ctx context.Context, rec StoreRecord) error

	// Get returns the current row for id. If the session has silently
	// crossed its idle or absolute deadline since it was last observed,
	// the transition (and its evidence) is durably persisted before the
	// updated row is returned -- the same [EvaluateExpiry] check
	// [Manager.Get] runs, applied to the durable row instead of an
	// in-memory one. Returns [ErrSessionNotFound] if id names no session.
	Get(ctx context.Context, id ID, now time.Time) (StoreRecord, error)

	// Touch applies the same expiry check as Get and, only if the session
	// is (still) active afterward, advances last_activity_at and returns
	// the updated row. Returns [ErrSessionNotActive] (wrapping the row's
	// resulting status) if expiry left it non-active.
	Touch(ctx context.Context, id ID, now time.Time) (StoreRecord, error)

	// Revoke applies the same expiry check as Get and, only if the session
	// is (still) active afterward, transitions it to StatusRevoked with
	// reason, recording REVOKED evidence. Returns [ErrSessionNotActive] if
	// expiry already left it non-active (matching [Manager.Revoke], which
	// never revokes a session that is not currently active).
	Revoke(ctx context.Context, id ID, reason string, now time.Time) (StoreRecord, error)

	// Refresh presents the hash of a raw refresh token, single-use exactly
	// as [Manager.Refresh] requires: if hash names the session's current
	// generation, it is retired and newHash takes its place atomically
	// (rotation_count and last_activity_at advance, a ROTATED evidence row
	// is recorded, and a new row is appended to the durable refresh-token
	// generation ledger under a unique constraint on the token hash itself
	// -- so two callers racing to rotate the same presented token collide
	// on that constraint, never on a read-then-write window). If hash is
	// recognized but no longer names the current generation -- a replay --
	// the whole session is revoked with [ReasonRefreshReplay] and
	// [ErrRefreshReplay] is returned. If hash was never issued at all,
	// [ErrRefreshUnknown] is returned. If the session's current generation
	// is not active (after the same expiry check Get applies),
	// [ErrSessionNotActive] is returned.
	Refresh(ctx context.Context, presentedHash, newHash string, now time.Time) (StoreRecord, error)

	// RecordEvidence durably appends one evidence row for id that is not
	// itself the byproduct of a state transition another Store method
	// already records -- used by [PersistentManager.Validate] to log a
	// Claim mismatch ([EvidenceDenied]), which denies the caller without
	// changing the session's own state.
	RecordEvidence(ctx context.Context, id ID, kind EvidenceKind, reason string, at time.Time) error

	// Evidence returns every durable evidence row for id, oldest first,
	// matching [Manager.Evidence]'s own ordering contract.
	Evidence(ctx context.Context, id ID) ([]Evidence, error)
}

// EvaluateExpiry decides whether a session currently in status, having last
// seen activity at lastActivityAt and carrying idleTimeout and
// absoluteExpiresAt, has silently crossed a deadline as of now. It is the
// exact decision [Manager.applyExpiryLocked] makes for the in-memory path,
// extracted here as a pure function precisely so a [Store] implementation
// in a different package (which cannot reach that unexported method) can
// apply the identical rule to a durably loaded row -- an in-memory session
// and a durably stored one can never disagree about when they expire,
// because both ask this same function the same question.
//
// ok is false, and next/reason are meaningless, when status is not
// [StatusActive] (nothing to expire; a terminal status stays exactly as it
// was found) or when no deadline has yet passed. When ok is true, next is
// the terminal status the caller must durably persist ([StatusExpiredAbsolute]
// or [StatusExpiredIdle]) alongside reason ([ReasonAbsoluteTimeout] or
// [ReasonIdleTimeout]) and an [EvidenceExpired] evidence row, exactly once.
func EvaluateExpiry(status Status, lastActivityAt, absoluteExpiresAt time.Time, idleTimeout time.Duration, now time.Time) (next Status, reason string, ok bool) {
	if status != StatusActive {
		return status, "", false
	}
	if !now.Before(absoluteExpiresAt) {
		return StatusExpiredAbsolute, ReasonAbsoluteTimeout, true
	}
	if now.Sub(lastActivityAt) > idleTimeout {
		return StatusExpiredIdle, ReasonIdleTimeout, true
	}
	return status, "", false
}

// PersistentManagerConfig configures a [PersistentManager].
type PersistentManagerConfig struct {
	// Now supplies the current time. Nil means time.Now.
	Now func() time.Time
	// IdleTimeout and AbsoluteTimeout are the defaults a [CreateSpec] falls
	// back to when it leaves either at zero, exactly like
	// [ManagerConfig]'s own fields.
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	// Store is the durable backing every operation delegates to. Required.
	Store Store
}

// PersistentManager is [Manager]'s durable counterpart (SECARCH-002): the
// same session lifecycle contract -- create, refresh rotation with replay
// detection, idle/absolute timeouts, revocation, an append-only evidence
// trail -- backed by a [Store] instead of an in-process map, so that state
// survives a process restart and is visible to every replica sharing the
// same store within whatever propagation window that [Store] implementation
// gives (a synchronous SQL backing, such as
// internal/trust/session/pgstore.PGStore, gives read-your-writes visibility
// on commit; there is no cache in front of it here for a replica to lag
// behind).
//
// PersistentManager exists alongside [Manager], not in place of it: [Manager]
// remains the in-memory reference implementation and every existing
// TRUST-003 test's contract; this type is the addition SECARCH-002 asks
// for, sharing every exported type, error and constant in this package so a
// caller comparing the two implementations' behavior is comparing identical
// vocabulary. Nothing in [Manager] or [Record] was modified to add this
// type.
type PersistentManager struct {
	now                    func() time.Time
	idleTimeoutDefault     time.Duration
	absoluteTimeoutDefault time.Duration
	store                  Store
}

// NewPersistentManager validates cfg and returns a [PersistentManager] over
// cfg.Store.
func NewPersistentManager(cfg PersistentManagerConfig) (*PersistentManager, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("session: persistent manager requires a Store")
	}
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
	return &PersistentManager{now: now, idleTimeoutDefault: idle, absoluteTimeoutDefault: absolute, store: cfg.Store}, nil
}

// Create opens a new active session and returns its record plus the raw
// refresh token that currently rotates it, exactly like [Manager.Create]:
// the same validation, the same defaulting of a zero idle/absolute timeout,
// the same one-time-only raw token.
func (m *PersistentManager) Create(ctx context.Context, spec CreateSpec) (Record, RefreshToken, error) {
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
	sr := StoreRecord{
		ID:                   id,
		Tenant:               spec.Tenant,
		Subject:              spec.Subject,
		PrincipalFingerprint: spec.PrincipalFingerprint,
		Assurance:            spec.Assurance,
		Status:               StatusActive,
		CreatedAt:            now,
		LastActivityAt:       now,
		IdleTimeout:          idle,
		AbsoluteExpiresAt:    now.Add(absolute),
		CurrentTokenHash:     hash,
		Generation:           0,
		Version:              1,
	}
	if err := m.store.Create(ctx, sr); err != nil {
		return Record{}, "", err
	}
	return sr.Record(), RefreshToken(raw), nil
}

// Get returns the current snapshot of a session, first re-evaluating
// whether it has silently crossed an idle or absolute deadline since it was
// last observed -- durably, via the [Store], exactly like [Manager.Get]
// does in memory.
func (m *PersistentManager) Get(ctx context.Context, id ID) (Record, error) {
	sr, err := m.store.Get(ctx, id, m.now().UTC())
	if err != nil {
		return Record{}, err
	}
	return sr.Record(), nil
}

// Touch records activity on a session, extending its idle window, and
// returns the updated record. It fails if the session is not active
// (expired or revoked), matching [Manager.Touch].
func (m *PersistentManager) Touch(ctx context.Context, id ID) (Record, error) {
	sr, err := m.store.Touch(ctx, id, m.now().UTC())
	return sr.Record(), err
}

// Validate checks a session is active and, when claim asserts a tenant or
// an assurance level, that the assertion matches the session's recorded
// value exactly -- the identical contract [Manager.Validate] documents,
// including that a mismatch is denied and durably audited rather than
// silently reconciled.
func (m *PersistentManager) Validate(ctx context.Context, id ID, claim Claim) (Record, error) {
	now := m.now().UTC()
	sr, err := m.store.Get(ctx, id, now)
	if err != nil {
		return Record{}, err
	}
	if sr.Status != StatusActive {
		return sr.Record(), fmt.Errorf("%w: %s", ErrSessionNotActive, sr.Status)
	}
	if claim.Tenant != "" && claim.Tenant != sr.Tenant {
		denied := fmt.Errorf("%w: session is %q, claim is %q", ErrTenantMismatch, sr.Tenant, claim.Tenant)
		if evErr := m.store.RecordEvidence(ctx, id, EvidenceDenied, ReasonTenantMismatch, now); evErr != nil {
			// The denial stands even when its audit row cannot be
			// written: the caller is still refused, and the evidence
			// failure travels with the denial instead of vanishing.
			return sr.Record(), errors.Join(denied, fmt.Errorf("session: record denial evidence: %w", evErr))
		}
		return sr.Record(), denied
	}
	if claim.Assurance != trust.AssuranceUnspecified && claim.Assurance != sr.Assurance {
		denied := fmt.Errorf("%w: session is %s, claim is %s", ErrAssuranceMismatch, sr.Assurance, claim.Assurance)
		if evErr := m.store.RecordEvidence(ctx, id, EvidenceDenied, ReasonAssuranceMismatch, now); evErr != nil {
			return sr.Record(), errors.Join(denied, fmt.Errorf("session: record denial evidence: %w", evErr))
		}
		return sr.Record(), denied
	}
	touched, err := m.store.Touch(ctx, id, now)
	if err != nil {
		return sr.Record(), err
	}
	return touched.Record(), nil
}

// Refresh presents a refresh token, rotating it -- or, if presented is a
// retired token this store already rotated past, revoking the whole
// session and returning [ErrRefreshReplay] -- exactly matching
// [Manager.Refresh]'s contract, durably.
func (m *PersistentManager) Refresh(ctx context.Context, presented RefreshToken) (Record, RefreshToken, error) {
	newRaw, newHash, err := newOpaqueToken()
	if err != nil {
		return Record{}, "", err
	}
	sr, err := m.store.Refresh(ctx, hashToken(string(presented)), newHash, m.now().UTC())
	if err != nil {
		return sr.Record(), "", err
	}
	return sr.Record(), RefreshToken(newRaw), nil
}

// Revoke transitions a session to StatusRevoked immediately, invalidating
// its current refresh token. reason defaults to [ReasonManualRevoke] when
// empty, matching [Manager.Revoke].
func (m *PersistentManager) Revoke(ctx context.Context, id ID, reason string) (Record, error) {
	if reason == "" {
		reason = ReasonManualRevoke
	}
	sr, err := m.store.Revoke(ctx, id, reason, m.now().UTC())
	return sr.Record(), err
}

// Evidence returns every durable evidence record for id, oldest first.
func (m *PersistentManager) Evidence(ctx context.Context, id ID) ([]Evidence, error) {
	return m.store.Evidence(ctx, id)
}
