// Package geofence is the application boundary for governed jobsite location
// evidence. It pins an authorized site boundary to a time session, evaluates
// one device observation at a time, and hands auto-clock-out transitions to a
// transaction-owning adapter. It never stores raw location history or accepts
// a boundary selected by a device.
package geofence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domaingeofence "github.com/monstercameron/human-capital-management-suite/internal/domains/geofence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
)

var (
	ErrInvalidRequest       = errors.New("application geofence: invalid request")
	ErrUnavailable          = errors.New("application geofence: required port unavailable")
	ErrBoundaryNotEffective = errors.New("application geofence: boundary is not effective")
	ErrScopeMismatch        = errors.New("application geofence: session scope mismatch")
)

// BoundarySource is the project/site authority. Implementations resolve a
// published, authorized boundary at decision time; callers never provide the
// boundary payload.
type BoundarySource interface {
	ResolveBoundary(context.Context, string, string, time.Time) (domaingeofence.Boundary, error)
}

// PinStore durably records the boundary revision selected for a session.
// Implementations must make Pin idempotent for the same session and reject a
// different pin after the session has begun.
type PinStore interface {
	Pin(context.Context, string, string, SessionPin) error
	Load(context.Context, string, string) (SessionPin, error)
}

// SessionSource reads the authoritative time-session state.
type SessionSource interface {
	Load(context.Context, string, string) (timesession.Session, error)
}

// Writer owns the commit boundary. Apply persists the session transition and
// the minimum retained evidence in one transaction. CommitAutoOut must CAS
// the matching open session, append the AUTO_OUT observation, and persist the
// timecard-review marker atomically; retries return the original receipt.
type Writer interface {
	Apply(context.Context, Transition) (timesession.Session, error)
	CommitAutoOut(context.Context, AutoOutCommand) (AutoOutReceipt, error)
}

// FollowUp is the post-commit notification/review boundary. Implementations
// should make both operations idempotent by session and observation identity.
type FollowUp interface {
	NotifyWorker(context.Context, WorkerNotification) error
	MarkReview(context.Context, ReviewMarker) error
}

// Policy contains application-owned exit governance not present in the
// geometric boundary itself.
type Policy struct {
	SustainedMinimum time.Duration
}

func (p Policy) validate() error {
	if p.SustainedMinimum < 0 {
		return fmt.Errorf("%w: sustained minimum cannot be negative", ErrInvalidRequest)
	}
	return nil
}

// Service composes the boundary, pin, session and commit ports. State lives
// in those ports, not in a package-level registry, so restart recovery uses
// the same durable facts.
type Service struct {
	Boundaries BoundarySource
	Pins       PinStore
	Sessions   SessionSource
	Writer     Writer
	FollowUp   FollowUp
	Policy     Policy
	Clock      func() time.Time
}

func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

// SessionPin is the immutable policy snapshot pinned to one time session.
type SessionPin struct {
	Tenant         string
	SessionID      string
	WorkerRef      string
	AssignmentRef  string
	SiteRef        string
	Boundary       domaingeofence.Boundary
	BoundaryDigest string
	PinnedAt       time.Time
}

// PinRequest identifies the authoritative site assignment for a new session.
type PinRequest struct {
	Tenant        string
	SessionID     string
	WorkerRef     string
	AssignmentRef string
	SiteRef       string
}

// PinSession resolves and pins the currently effective site boundary. The
// selected revision remains authoritative even if a newer site policy is
// published later.
func (s Service) PinSession(ctx context.Context, req PinRequest) (SessionPin, error) {
	if strings.TrimSpace(req.Tenant) == "" || strings.TrimSpace(req.SessionID) == "" ||
		strings.TrimSpace(req.WorkerRef) == "" || strings.TrimSpace(req.AssignmentRef) == "" || strings.TrimSpace(req.SiteRef) == "" {
		return SessionPin{}, ErrInvalidRequest
	}
	if s.Boundaries == nil || s.Pins == nil {
		return SessionPin{}, ErrUnavailable
	}
	now := s.now()
	if now.IsZero() {
		return SessionPin{}, ErrUnavailable
	}
	boundary, err := s.Boundaries.ResolveBoundary(ctx, req.Tenant, req.SiteRef, now)
	if err != nil {
		return SessionPin{}, err
	}
	if boundary.SiteRef != req.SiteRef {
		return SessionPin{}, ErrScopeMismatch
	}
	effective, err := boundary.EffectiveAt(now)
	if err != nil {
		return SessionPin{}, err
	}
	if !effective {
		return SessionPin{}, ErrBoundaryNotEffective
	}
	digest, err := boundary.Digest()
	if err != nil {
		return SessionPin{}, err
	}
	pin := SessionPin{
		Tenant: req.Tenant, SessionID: req.SessionID, WorkerRef: req.WorkerRef,
		AssignmentRef: req.AssignmentRef, SiteRef: req.SiteRef, Boundary: boundary,
		BoundaryDigest: digest, PinnedAt: now,
	}
	if err := s.Pins.Pin(ctx, req.Tenant, req.SessionID, pin); err != nil {
		return SessionPin{}, err
	}
	return pin, nil
}

// EvidenceInput is the device observation. ReceivedAt is intentionally absent:
// the service stamps it from its server clock.
type EvidenceInput struct {
	Observation domaingeofence.DeviceObservation
	EvidenceRef string
	Sustained   time.Duration
	// AmbiguousIndoor is a provider confidence fact. A coordinate that cannot
	// distinguish an indoor fix from a nearby boundary is always unknown.
	AmbiguousIndoor bool
}

// EvidenceRecord is the minimum retained proof. It includes receipt and
// observation instants separately, but never contains the raw coordinate.
type EvidenceRecord struct {
	Tenant         string
	SessionID      string
	SiteRef        string
	BoundaryDigest string
	EvidenceRef    string
	Source         string
	ConsentRef     string
	Confidence     string
	Status         domaingeofence.Status
	Reason         domaingeofence.UnknownReason
	AccuracyMeters float64
	ObservedAt     time.Time
	ReceivedAt     time.Time
	EvaluatedAt    time.Time
	RetainUntil    time.Time
}

// Transition is the atomic session/evidence write for one evaluated signal.
type Transition struct {
	Tenant           string
	SessionID        string
	ExpectedRevision uint64
	Session          timesession.Session
	Event            Event
	Evidence         EvidenceRecord
}

// Event is the audit-safe transition metadata. Payload is derived only from
// the minimum proof and therefore cannot accidentally retain raw coordinates.
type Event struct {
	Kind           string
	IdempotencyKey string
	Digest         string
	Payload        []byte
}

// ExitResult is the worker/session projection after one evidence signal.
type ExitResult struct {
	Evaluation domaingeofence.Evaluation
	Decision   timesession.ExitDecision
	Session    timesession.Session
	Evidence   EvidenceRecord
}

// ObserveExit evaluates one server-received fix against the session's pinned
// boundary. Unknown evidence is recorded as an explicit exception and cannot
// start a grace timer.
func (s Service) ObserveExit(ctx context.Context, tenant, sessionID string, input EvidenceInput) (ExitResult, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(input.EvidenceRef) == "" || input.Sustained < 0 {
		return ExitResult{}, ErrInvalidRequest
	}
	if s.Pins == nil || s.Sessions == nil || s.Writer == nil {
		return ExitResult{}, ErrUnavailable
	}
	if err := s.Policy.validate(); err != nil {
		return ExitResult{}, err
	}
	now := s.now()
	if now.IsZero() {
		return ExitResult{}, ErrUnavailable
	}
	pin, err := s.Pins.Load(ctx, tenant, sessionID)
	if err != nil {
		return ExitResult{}, err
	}
	if err := validatePin(pin, tenant, sessionID); err != nil {
		return ExitResult{}, err
	}
	session, err := s.Sessions.Load(ctx, tenant, sessionID)
	if err != nil {
		return ExitResult{}, err
	}
	if session.Tenant != tenant || session.SessionID != sessionID || session.Worker == "" || session.Assignment == "" || session.Worker != pin.WorkerRef || session.Assignment != pin.AssignmentRef {
		return ExitResult{}, ErrScopeMismatch
	}
	evaluation, err := domaingeofence.Evaluate(pin.Boundary, domaingeofence.ServerEvidence{
		Observation: input.Observation, ReceivedAt: now, EvidenceRef: input.EvidenceRef,
	}, now)
	if err != nil {
		return ExitResult{}, err
	}
	if input.AmbiguousIndoor {
		evaluation.Status = domaingeofence.StatusUnknown
		evaluation.Reason = domaingeofence.UnknownReason("AMBIGUOUS_INDOOR")
		evaluation.DistanceMeters = 0
		evaluation.HasDistance = false
	}
	proof, err := domaingeofence.Retain(pin.Boundary, evaluation, input.EvidenceRef)
	if err != nil {
		return ExitResult{}, err
	}
	decisionEvidence := timesession.ExitEvidence{
		Consent:        input.Observation.PermissionGranted && strings.TrimSpace(input.Observation.ConsentRef) != "",
		AccuracyMeters: input.Observation.AccuracyMeters, SustainedDuration: input.Sustained,
		Source: string(input.Observation.Source), Exited: evaluation.Status == domaingeofence.StatusOutside,
	}
	// Unknown is a visible exception, never a silent no-op. It is presented to
	// the pure exit kernel as an exited-but-untrusted signal so no grace starts.
	if evaluation.Status == domaingeofence.StatusUnknown {
		decisionEvidence.Exited = true
		decisionEvidence.Consent = false
	}
	next, decision, err := timesession.EvaluateExit(timesession.ExitPolicy{
		AccuracyThresholdMeters: pin.Boundary.AccuracyThresholdMeters,
		SustainedMinimum:        s.Policy.SustainedMinimum, GraceDuration: pin.Boundary.ExitGrace,
	}, decisionEvidence, session, now)
	if err != nil {
		return ExitResult{}, err
	}
	record := EvidenceRecord{
		Tenant: tenant, SessionID: sessionID, SiteRef: pin.SiteRef, BoundaryDigest: pin.BoundaryDigest,
		EvidenceRef: input.EvidenceRef, Source: string(input.Observation.Source), ConsentRef: input.Observation.ConsentRef,
		Confidence: confidence(evaluation.Status), Status: evaluation.Status, Reason: evaluation.Reason,
		AccuracyMeters: input.Observation.AccuracyMeters, ObservedAt: input.Observation.ObservedAt.UTC(),
		ReceivedAt: now, EvaluatedAt: evaluation.EvaluatedAt, RetainUntil: proof.RetainUntil,
	}
	event := eventFor("GEOFENCE_"+string(decision.Kind), input.EvidenceRef, record)
	saved, err := s.Writer.Apply(ctx, Transition{Tenant: tenant, SessionID: sessionID, ExpectedRevision: session.Revision, Session: next, Event: event, Evidence: record})
	if err != nil {
		return ExitResult{}, err
	}
	return ExitResult{Evaluation: evaluation, Decision: decision, Session: saved, Evidence: record}, nil
}

// AutoOutCommand is the atomic server-owned expiry request.
type AutoOutCommand struct {
	Tenant           string
	SessionID        string
	WorkerRef        string
	AssignmentRef    string
	ExpectedRevision uint64
	Session          timesession.Session
	Event            Event
	Observation      AutoOutObservation
	Evidence         EvidenceRecord
}

// AutoOutObservation is the immutable AUTO_OUT observation projection.
type AutoOutObservation struct {
	ID             string
	EventType      string
	IdempotencyKey string
	OccurredAt     time.Time
	ReceivedAt     time.Time
	BoundaryDigest string
	EvidenceRef    string
}

type AutoOutReceipt struct {
	Session        timesession.Session
	ObservationID  string
	Duplicate      bool
	ReviewRequired bool
}

// WorkerNotification is deliberately bounded to the time decision; it is not
// a general location-tracking notification.
type WorkerNotification struct {
	Tenant, WorkerRef, SessionID, ObservationID string
	Kind                                        string
}

type ReviewMarker struct {
	Tenant, WorkerRef, SessionID, ObservationID, Reason string
}

// ExpireAutoOut closes a session only after its persisted server-owned grace
// expires. The writer is the atomic/replay-safe commit boundary.
func (s Service) ExpireAutoOut(ctx context.Context, tenant, sessionID string) (AutoOutReceipt, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(sessionID) == "" {
		return AutoOutReceipt{}, ErrInvalidRequest
	}
	if s.Pins == nil || s.Sessions == nil || s.Writer == nil || s.FollowUp == nil {
		return AutoOutReceipt{}, ErrUnavailable
	}
	now := s.now()
	if now.IsZero() {
		return AutoOutReceipt{}, ErrUnavailable
	}
	pin, err := s.Pins.Load(ctx, tenant, sessionID)
	if err != nil {
		return AutoOutReceipt{}, err
	}
	if err := validatePin(pin, tenant, sessionID); err != nil {
		return AutoOutReceipt{}, err
	}
	session, err := s.Sessions.Load(ctx, tenant, sessionID)
	if err != nil {
		return AutoOutReceipt{}, err
	}
	if session.Tenant != tenant || session.SessionID != sessionID || session.Worker != pin.WorkerRef || session.Assignment != pin.AssignmentRef {
		return AutoOutReceipt{}, ErrScopeMismatch
	}
	key := stableID(tenant, sessionID, "AUTO_OUT", pin.BoundaryDigest)
	next, outcome, err := timesession.ExpireGrace(session, now, key)
	if err != nil {
		return AutoOutReceipt{}, err
	}
	closeAt := now
	if len(next.Segments) > 0 {
		closeAt = next.Segments[len(next.Segments)-1].End
	}
	observationID := stableID(tenant, sessionID, "AUTO_OUT_OBSERVATION", key)
	evidenceRef := stableID(tenant, sessionID, "AUTO_OUT_EVIDENCE", key)
	evidence := EvidenceRecord{
		Tenant: tenant, SessionID: sessionID, SiteRef: pin.SiteRef, BoundaryDigest: pin.BoundaryDigest,
		EvidenceRef: evidenceRef, Source: "GEOFENCE", Confidence: "SUFFICIENT",
		Status: domaingeofence.StatusOutside, Reason: domaingeofence.ReasonNone,
		ObservedAt: closeAt, ReceivedAt: now, EvaluatedAt: now, RetainUntil: now.Add(pin.Boundary.RetentionPeriod),
	}
	event := eventFor("AUTO_OUT", key, evidence)
	receipt, err := s.Writer.CommitAutoOut(ctx, AutoOutCommand{
		Tenant: tenant, SessionID: sessionID, WorkerRef: session.Worker, AssignmentRef: session.Assignment,
		ExpectedRevision: session.Revision, Session: next, Event: event,
		Observation: AutoOutObservation{ID: observationID, EventType: "AUTO_OUT", IdempotencyKey: key, OccurredAt: closeAt, ReceivedAt: now, BoundaryDigest: pin.BoundaryDigest, EvidenceRef: evidenceRef},
		Evidence:    evidence,
	})
	if err != nil {
		return AutoOutReceipt{}, err
	}
	if receipt.Session.Tenant != tenant || receipt.Session.SessionID != sessionID || receipt.Session.Worker != session.Worker || receipt.Session.Assignment != session.Assignment || receipt.Session.State != timesession.StateAutoClosed || receipt.Session.Revision != next.Revision || receipt.ObservationID != observationID {
		return AutoOutReceipt{}, fmt.Errorf("%w: auto-out writer returned an invalid receipt", ErrScopeMismatch)
	}
	if receipt.Duplicate {
		return receipt, nil
	}
	if err := s.FollowUp.NotifyWorker(ctx, WorkerNotification{Tenant: tenant, WorkerRef: session.Worker, SessionID: sessionID, ObservationID: observationID, Kind: "AUTO_OUT_PROVISIONAL"}); err != nil {
		return receipt, err
	}
	if err := s.FollowUp.MarkReview(ctx, ReviewMarker{Tenant: tenant, WorkerRef: session.Worker, SessionID: sessionID, ObservationID: observationID, Reason: "AUTO_OUT_PROVISIONAL"}); err != nil {
		return receipt, err
	}
	receipt.ReviewRequired = true
	_ = outcome // the writer owns the durable projection; outcome validates the transition above.
	return receipt, nil
}

func validatePin(pin SessionPin, tenant, sessionID string) error {
	if pin.Tenant != tenant || pin.SessionID != sessionID || pin.WorkerRef == "" || pin.AssignmentRef == "" || pin.SiteRef == "" || pin.PinnedAt.IsZero() {
		return ErrScopeMismatch
	}
	digest, err := pin.Boundary.Digest()
	if err != nil || digest != pin.BoundaryDigest {
		return ErrScopeMismatch
	}
	return nil
}

func confidence(status domaingeofence.Status) string {
	if status == domaingeofence.StatusInside || status == domaingeofence.StatusOutside {
		return "SUFFICIENT"
	}
	return "UNKNOWN"
}

func eventFor(kind, key string, evidence EvidenceRecord) Event {
	payload, _ := json.Marshal(struct {
		Tenant, SessionID, SiteRef, BoundaryDigest, EvidenceRef, Source, ConsentRef, Confidence string
		Status                                                                                  domaingeofence.Status
		Reason                                                                                  domaingeofence.UnknownReason
		ObservedAt, ReceivedAt, EvaluatedAt, RetainUntil                                        time.Time
	}{evidence.Tenant, evidence.SessionID, evidence.SiteRef, evidence.BoundaryDigest, evidence.EvidenceRef, evidence.Source, evidence.ConsentRef, evidence.Confidence, evidence.Status, evidence.Reason, evidence.ObservedAt, evidence.ReceivedAt, evidence.EvaluatedAt, evidence.RetainUntil})
	hash := sha256.Sum256(append([]byte(kind+"\x00"+key+"\x00"), payload...))
	return Event{Kind: kind, IdempotencyKey: key, Digest: "sha256:" + hex.EncodeToString(hash[:]), Payload: payload}
}

func stableID(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "geofence:" + hex.EncodeToString(hash[:])
}
