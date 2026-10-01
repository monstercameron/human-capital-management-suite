package clockservice

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// -----------------------------------------------------------------------
// Durable record shapes.
//
// These mirror the row shapes internal/data/timestore is building in
// parallel (SessionRow, ObservationRow, ReceiptRow, Device, ...) closely
// enough that an adapter from a timestore.Store method to a port method
// here is a field-for-field copy, never a redesign. This package does not
// import internal/data/timestore: the composition root supplies that
// adapter.
// -----------------------------------------------------------------------

// SessionRecord is the durable projection of one timesession.Session.
type SessionRecord struct {
	ID, TenantID, WorkerRef, AssignmentRef, Status, Source, ProjectRef string
	Revision                                                           uint64
	OpenedAt, ClosedAt                                                 time.Time
	Payload                                                            []byte
}

// SessionEvent is one entry in a session's append-only transition journal.
type SessionEvent struct {
	Kind, ActorRef, IdempotencyKey, Digest string
	Payload                                []byte
}

// ObservationRecord is one immutable signed punch observation.
type ObservationRecord struct {
	ID, TenantID, WorkerRef, AssignmentRef, DeviceRef, Source, EventType, ProjectRef, Timezone string
	IdempotencyKey, Digest, CorrectsID                                                         string
	OccurredAt, ReceivedAt                                                                     time.Time
	Payload                                                                                    []byte
}

// ReceiptRecord is the immutable per-punch receipt for one device batch
// entry, keyed by device sequence.
type ReceiptRecord struct {
	Status, Reason, ObservationID string
	DeviceSequence                int64
	Payload                       []byte
}

// DeviceRecord is the current-state row for one enrolled clock or kiosk.
type DeviceRecord struct {
	TenantID, ID                       string
	PublicKey                          []byte
	SiteID, ProfileID, Timezone, State string
	Revision                           int64
}

// CredentialRecord is the current state of one worker identification
// credential (PIN, badge or QR) at shared devices.
type CredentialRecord struct {
	ID, WorkerID, Kind, ExternalID, State string
	Revision                              int64
}

// LockoutState is a device's or worker's current failed-attempt counter
// and lockout deadline.
type LockoutState struct {
	FailedCount int
	LockedUntil time.Time
}

// SupervisorOverrideRecord is one append-only record of a supervisor
// authorizing an identification that would otherwise be refused.
type SupervisorOverrideRecord struct {
	ID, DeviceID, WorkerID, SupervisorCredentialRef, Reason string
	CreatedAt                                               time.Time
}

// HeartbeatRecord is one device fleet-health report.
type HeartbeatRecord struct {
	DeviceID, AppVersion, PowerState   string
	QueueDepth, OldestUnsentAgeSeconds int
	BatteryPercent                     int
	HasBatteryPercent                  bool
	OffsetMillis                       int64
	ObservedAt                         time.Time
}

// OutboxEvent is one committed clock/timecard event, read back for
// diagnostics and for TestTodo_WTIME_004_Fault-style single-commit proofs.
type OutboxEvent struct {
	Sequence      int64
	EventType     string
	SchemaVersion int
	Payload       []byte
	CreatedAt     time.Time
}

// RosterWorker is one eligible worker projected for a device's site,
// carrying credential VERIFIERS only (a salted PIN hash, a badge
// identifier, a QR key id) and never a raw secret.
type RosterWorker struct {
	WorkerRef, DisplayName string
	PINHash, PINSalt       []byte
	BadgeID, QRKeyID       string
	Terminated             bool
}

// JobCode is one job or cost code a device may attribute a segment to.
type JobCode struct {
	Code, Name string
}

// PublishedShift is the minimum a device needs to enforce a lockout
// window: which worker, and the shift's start and end.
type PublishedShift struct {
	WorkerRef, ShiftID string
	Start, End         time.Time
}

// TipRules is the descriptive tip-declaration policy shown at a device.
type TipRules struct {
	Enabled  bool
	Currency string
}

// RosterDelta is TCLOCK-004's cursor-based sync response for one device.
type RosterDelta struct {
	SnapshotRevision   string
	Workers            []RosterWorker
	JobCodes           []JobCode
	Shifts             []PublishedShift
	Questions          punchpolicy.QuestionSet
	Tips               TipRules
	Strings            map[string]string
	PunchPolicyVersion int
	MaxOfflineAge      time.Duration
	UrgentRemovals     []string
	NextCursor         string
	HasMore            bool
}

// -----------------------------------------------------------------------
// Ports. Every one is a small, service-owned interface; the composition
// root supplies the concrete adapter (typically backed by
// internal/data/timestore). Unit tests in this package use in-memory
// fakes.
// -----------------------------------------------------------------------

// SessionStore owns durable clock-session state. OpenSession and
// ApplyTransition each commit in one store transaction together with the
// session's transition events; a retry sharing an event's idempotency key
// replays the original result rather than erroring.
type SessionStore interface {
	OpenSession(ctx context.Context, tenant string, session SessionRecord, event SessionEvent) (SessionRecord, error)
	ApplyTransition(ctx context.Context, tenant, sessionID string, expectedRevision uint64, next SessionRecord, events []SessionEvent) (SessionRecord, error)
	CurrentSession(ctx context.Context, tenant, worker, assignment string) (SessionRecord, error)
}

// ObservationStore persists immutable signed punch observations, keyed by
// (tenant, source, idempotency key) so a retried submission returns the
// original row rather than creating a second one.
type ObservationStore interface {
	AppendObservation(ctx context.Context, tenant string, obs ObservationRecord) (ObservationRecord, bool, error)
	ListObservations(ctx context.Context, tenant, worker string, from, to time.Time, cursor string, limit int) ([]ObservationRecord, string, error)
}

// ReceiptStore persists one device batch's per-punch receipts in one store
// transaction per batch chunk, keyed by device sequence so a retried
// sequence number always resolves to its original receipt.
type ReceiptStore interface {
	RecordBatch(ctx context.Context, tenant, deviceID string, batch []ReceiptRecord) ([]ReceiptRecord, int64, error)
	DeviceCursor(ctx context.Context, tenant, deviceID string) (int64, error)
}

// DeviceStore owns the enrolled device's current state and its revisioned
// transitions (TCLOCK-002).
type DeviceStore interface {
	GetDevice(ctx context.Context, tenant, id string) (DeviceRecord, error)
	DevicesBySite(ctx context.Context, tenant, siteID string, limit int) ([]DeviceRecord, error)
	RotateDeviceKey(ctx context.Context, tenant, id string, newPublicKey []byte, actorID, reason string, expectedRevision int64) (DeviceRecord, error)
	SuspendDevice(ctx context.Context, tenant, id, actorID, reason string, expectedRevision int64) (DeviceRecord, error)
	ResumeDevice(ctx context.Context, tenant, id, actorID, reason string, expectedRevision int64) (DeviceRecord, error)
	RevokeDevice(ctx context.Context, tenant, id, actorID, reason string, expectedRevision int64) (DeviceRecord, error)
	ReassignDeviceSite(ctx context.Context, tenant, id, newSiteID, newTimezone, actorID, reason string, expectedRevision int64) (DeviceRecord, error)
}

// EnrollmentStore owns the single-use enrollment code lifecycle and the
// device birth it gates (TCLOCK-002).
type EnrollmentStore interface {
	CreateEnrollmentCode(ctx context.Context, tenant, code, siteID, profileID, timezone, createdBy string, expiresAt time.Time) error
	RedeemEnrollmentCode(ctx context.Context, tenant, code, deviceID string, publicKey []byte, actorID string, now time.Time) (DeviceRecord, error)
}

// CredentialStore owns worker identification credentials, their rate-limit
// windows, and supervisor override evidence (TCLOCK-005).
type CredentialStore interface {
	IssuePINCredential(ctx context.Context, tenant, workerID, pin, actorID string) (CredentialRecord, error)
	IssueBadgeCredential(ctx context.Context, tenant, workerID, badgeID, actorID string) (CredentialRecord, error)
	IssueQRCredential(ctx context.Context, tenant, workerID, qrKeyID, actorID string) (CredentialRecord, error)
	RevokeCredential(ctx context.Context, tenant, workerID, kind, actorID string, expectedRevision int64) (CredentialRecord, error)
	VerifyPIN(ctx context.Context, tenant, workerID, pin string) (bool, error)
	RecordFailedDeviceAttempt(ctx context.Context, tenant, deviceID string, threshold int, lockUntil time.Time) (int, error)
	RecordFailedWorkerAttempt(ctx context.Context, tenant, workerID string, threshold int, lockUntil time.Time) (int, error)
	ResetDeviceAttempts(ctx context.Context, tenant, deviceID string) error
	ResetWorkerAttempts(ctx context.Context, tenant, workerID string) error
	DeviceLockout(ctx context.Context, tenant, deviceID string) (LockoutState, error)
	WorkerLockout(ctx context.Context, tenant, workerID string) (LockoutState, error)
	RecordSupervisorOverride(ctx context.Context, tenant, deviceID, workerID, supervisorCredentialRef, reason string) (SupervisorOverrideRecord, error)
}

// HeartbeatStore owns device fleet-health reporting (TCLOCK-008).
type HeartbeatStore interface {
	RecordHeartbeat(ctx context.Context, tenant string, hb HeartbeatRecord) error
	LatestHeartbeat(ctx context.Context, tenant, deviceID string) (HeartbeatRecord, error)
}

// RosterSource builds TCLOCK-004's cursor-based delta feed from the
// published roster and policy read models. It is a read model, not a
// worker-record copy: the service never asks it to persist anything.
type RosterSource interface {
	Delta(ctx context.Context, tenant, siteID, deviceProfileID, cursor string, now time.Time) (RosterDelta, error)
}

// PolicySource resolves the pinned, versioned punch policy and attestation
// question set in effect for a site (TCLOCK-009/TCLOCK-010's policy data).
type PolicySource interface {
	PunchPolicy(ctx context.Context, tenant, siteID string) (punchpolicy.Policy, error)
	AttestationQuestions(ctx context.Context, tenant, siteID string) (punchpolicy.QuestionSet, error)
}

// ProfileResolver resolves the TimeProfile that governs a worker's
// assignment at a given instant (internal/domains/timeprofile).
type ProfileResolver interface {
	Resolve(ctx context.Context, tenant, workerRef, assignmentRef string, at time.Time) (timeprofile.TimeProfile, error)
}

// WorkerDirectory is backed by the authoritative HCM worker identity and
// assignment source. Results must be tenant scoped and decision-time
// current; nothing here is inferred from a client-supplied display name.
type WorkerDirectory interface {
	ResolveWorker(ctx context.Context, tenant, claimedWorkerRef string) (workerRef string, active bool, err error)
	ResolveAssignment(ctx context.Context, tenant, workerRef, assignmentRef string) (projectRef, siteRef string, ok bool, err error)
}

// Authorizer evaluates current authority on every call; it must fail
// closed and must never trust a client-supplied role or delegation claim.
type Authorizer interface {
	// AuthorizePunch reports whether actor may act for workerRef on
	// assignmentRef. delegated is true only when a current delegation
	// grant (not a client-asserted flag) authorizes actor to act for a
	// different worker.
	AuthorizePunch(ctx context.Context, p *trust.Principal, tenant, workerRef, assignmentRef string) (delegated bool, err error)
	// AuthorizeDeviceAdmin gates enrollment, credential and fleet
	// management actions for a site.
	AuthorizeDeviceAdmin(ctx context.Context, p *trust.Principal, tenant, siteID string) error
	// AuthorizeSupervisorOverride reports whether p currently holds a
	// supervisor role able to clear a lockout or identification override
	// at siteID.
	AuthorizeSupervisorOverride(ctx context.Context, p *trust.Principal, tenant, siteID string) error
}

// SessionWorkflow is WTIME-003's hook: every accepted punch is handed to
// the session workflow after its observation and session transition
// commit. StartRun begins a new run for an IN punch; SignalRun correlates
// every later punch (break, transfer, out) onto the run its session
// opened. Continuation past this call is asynchronous: a failure here
// never rolls back or blocks the already-committed receipt.
type SessionWorkflow interface {
	StartRun(ctx context.Context, tenant, sessionID, workerRef, assignmentRef, observationID string, at time.Time) error
	SignalRun(ctx context.Context, tenant, sessionID, signal, observationID string, at time.Time) error
}

// EventOutbox reads back committed clock events for diagnostics and
// recovery proofs.
type EventOutbox interface {
	ListEvents(ctx context.Context, tenant string, afterCursor int64, limit int) ([]OutboxEvent, error)
}

// PremiumInputs is TCLOCK-010's dispatch target for a "break not provided"
// attestation answer (REV-045-01). This package never calculates or posts
// the premium itself.
type PremiumInputs interface {
	RequestPremiumInput(ctx context.Context, tenant, workerRef, sessionID, ruleRef, reason string, at time.Time) error
}

// CaseTasks is TCLOCK-010's dispatch target for an injury attestation
// answer. This package never opens the case itself.
type CaseTasks interface {
	OpenCaseTask(ctx context.Context, tenant, workerRef, sessionID, reason, severity string) error
}

// PunchWork is everything one accepted punch must commit atomically: the
// resulting session projection and its transition events, plus the raw
// observation. UnitOfWork.Punch is the ONE store transaction per punch
// this package's engineering bar requires.
type PunchWork struct {
	Session          SessionRecord
	SessionIsNew     bool // true for an IN punch: OpenSession, not ApplyTransition.
	ExpectedRevision uint64
	// ExpectedProjectionRevision is the worker/assignment clock cursor, which
	// is independent of the current session's revision.
	ExpectedProjectionRevision uint64
	SessionEvents              []SessionEvent
	Observation                ObservationRecord
}

// PunchResult is UnitOfWork.Punch's outcome: the persisted session and
// observation, and whether the observation was a replayed duplicate.
type PunchResult struct {
	Session     SessionRecord
	Observation ObservationRecord
	Duplicate   bool
}

// UnitOfWork is the seam a DB adapter binds to make one punch's session
// transition, observation and outbox event commit in exactly one store
// transaction (WTIME-004's "single commit of observation + session
// transition + outbox"), and one device batch chunk commit in exactly one
// transaction. An overloaded implementation returns ErrRetryLater; the
// punch is then guaranteed not committed, so the caller can safely retry
// with the same idempotency key.
type UnitOfWork interface {
	Punch(ctx context.Context, tenant string, work PunchWork) (PunchResult, error)
	Batch(ctx context.Context, tenant string, work []PunchWork) ([]PunchResult, error)
}

// IDs generates the identities this service needs. Deterministic ids make
// a client retry of the same logical create idempotent without a
// database round trip to check; Random ids are used where no such replay
// semantics apply (for example a receipt-only artifact).
type IDs interface {
	Deterministic(parts ...string) string
	Random() string
}
