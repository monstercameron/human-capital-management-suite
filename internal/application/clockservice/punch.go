// Punch acceptance runs through the published workflow commit node before returning a receipt.
package clockservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ReceiptStatus is the closed vocabulary a PunchReceipt reports. There is
// deliberately no generic "pending": WTIME-004 requires the receipt to say
// accepted, duplicate or retry-later, never anything in between.
type ReceiptStatus string

const (
	ReceiptAccepted   ReceiptStatus = "ACCEPTED"
	ReceiptDuplicate  ReceiptStatus = "DUPLICATE"
	ReceiptRetryLater ReceiptStatus = "RETRY_LATER"
)

// ErrPunchIdempotencyConflict means an idempotency key was already accepted
// for different immutable punch input. It is deliberately separate from a
// session-state error so callers never turn a changed retry into a new punch.
var ErrPunchIdempotencyConflict = errors.New("clockservice: punch idempotency conflict")

// PunchRequest is one FTIME-003 punch: an IN, OUT, break/meal start or end,
// or a job/cost-code transfer, for the authenticated worker or, when
// Actor differs from ClaimedWorkerRef, a delegated supervisor.
type PunchRequest struct {
	ClaimedWorkerRef string
	AssignmentRef    string
	JobRef           string
	CostCodeRef      string
	Travel           bool

	DeviceKind string // KIOSK, MOBILE, HARDWARE_CLOCK, IMPORT, WEB, ...
	DeviceRef  string
	Method     timesession.IdentificationMethod

	DeviceTime     time.Time
	IdempotencyKey string
	// ExpectedProjectionRevision fences first-party actions against the durable
	// worker/assignment clock projection in the same transaction as the punch.
	ExpectedProjectionRevision uint64

	// Scheduled is the caller's resolved fact that this punch falls inside
	// a published shift. It is never inferred here: an omitted or false
	// value is captured as an UNSCHEDULED_WORK exception, never rejected.
	Scheduled bool
}

// PunchReceipt is the open-session state and typed outcome FTIME-003
// returns for one punch.
type PunchReceipt struct {
	Status                  ReceiptStatus
	SessionID               string
	WorkerRef               string
	AssignmentRef           string
	State                   timesession.SessionState
	Revision                uint64
	Outcome                 timesession.OutcomeKind
	Exception               *timesession.Exception
	ObservationID           string
	WorkflowInstanceRef     string
	WorkflowID              string
	WorkflowPlanDigest      string
	WorkflowStartKey        string
	WorkflowTraceID         string
	WorkflowNodeID          string
	WorkflowAttempt         int
	WorkflowInstanceVersion int64
}

// PunchObservationLookup is an optional direct recovery seam. Adapters
// should implement it with the tenant/source/idempotency unique key so
// replay does not scan a worker's complete observation history.
type PunchObservationLookup interface {
	LookupPunchObservation(context.Context, string, string, string) (ObservationRecord, bool, error)
}

func (s Service) resolveWorkerAndAssignment(ctx context.Context, p *trust.Principal, tenant, claimedWorkerRef, assignmentRef string) (workerRef, projectRef string, delegated bool, err error) {
	if s.Workers == nil || s.Auth == nil {
		return "", "", false, ErrUnavailable
	}
	workerRef, active, err := s.Workers.ResolveWorker(ctx, tenant, claimedWorkerRef)
	if err != nil {
		return "", "", false, err
	}
	if !active || workerRef == "" {
		return "", "", false, reject(ErrWorkerNotEligible, "claimed_worker_ref", "", "worker does not resolve to an active worker")
	}
	projectRef, _, ok, err := s.Workers.ResolveAssignment(ctx, tenant, workerRef, assignmentRef)
	if err != nil {
		return "", "", false, err
	}
	if !ok {
		return "", "", false, reject(ErrAssignmentNotFound, "assignment_ref", "", "assignment does not resolve for this worker")
	}
	delegated, err = s.Auth.AuthorizePunch(ctx, p, tenant, workerRef, assignmentRef)
	if err != nil {
		return "", "", false, err
	}
	if p.Subject() != workerRef && !delegated {
		return "", "", false, reject(ErrDelegationRequired, "actor", "", fmt.Sprintf("actor=%s worker=%s", p.Subject(), workerRef))
	}
	return workerRef, projectRef, delegated, nil
}

// ClockIn starts a new clock session for the resolved worker at the
// resolved assignment.
func (s Service) ClockIn(ctx context.Context, p *trust.Principal, req PunchRequest) (PunchReceipt, error) {
	return s.punch(ctx, p, timesession.PunchIn, req)
}

// StartBreak opens a BREAK segment on the worker's open session.
func (s Service) StartBreak(ctx context.Context, p *trust.Principal, req PunchRequest) (PunchReceipt, error) {
	return s.punch(ctx, p, timesession.PunchBreakStart, req)
}

// EndBreak closes an open BREAK segment and resumes work.
func (s Service) EndBreak(ctx context.Context, p *trust.Principal, req PunchRequest) (PunchReceipt, error) {
	return s.punch(ctx, p, timesession.PunchBreakEnd, req)
}

// TransferJob closes the open segment and opens a new one against a
// different job or cost code (or travel) on the same session.
func (s Service) TransferJob(ctx context.Context, p *trust.Principal, req PunchRequest) (PunchReceipt, error) {
	return s.punch(ctx, p, timesession.PunchTransfer, req)
}

// ClockOutRequest is FTIME-003's OUT punch, extended with TCLOCK-010's
// clock-out attestation answers and tip declaration. Both are optional:
// omitting them clocks the worker out with no attestation evidence, which
// a jurisdiction's own policy (not this call) may separately flag.
type ClockOutRequest struct {
	PunchRequest
	Answers   []punchAnswer
	TipAmount string // decimal text; empty means no tip declared.
	SiteID    string
}

type punchAnswer struct {
	QuestionID string
	Value      string
}

// ClockOut closes the worker's open session and, when the caller supplied
// clock-out attestation answers, dispatches TCLOCK-010's typed
// consequences (a premium-pay input request, a case task) through their
// ports. A dispatch failure is reported on the receipt but never unwinds
// the already-committed OUT punch: the punch is the authoritative fact,
// the consequence is a best-effort follow-on.
func (s Service) ClockOut(ctx context.Context, p *trust.Principal, req ClockOutRequest) (PunchReceipt, error) {
	receipt, err := s.punch(ctx, p, timesession.PunchOut, req.PunchRequest)
	if err != nil {
		return receipt, err
	}
	if receipt.Status == ReceiptDuplicate {
		return receipt, nil
	}
	if len(req.Answers) == 0 && strings.TrimSpace(req.TipAmount) == "" {
		return receipt, nil
	}
	if err := s.dispatchClockOutConsequences(ctx, tenantOf(p), receipt, req); err != nil {
		return receipt, err
	}
	return receipt, nil
}

func (s Service) punch(ctx context.Context, p *trust.Principal, kind timesession.PunchKind, req PunchRequest) (PunchReceipt, error) {
	if err := validPrincipal(p); err != nil {
		return PunchReceipt{}, err
	}
	if s.Sessions == nil || s.Observations == nil || s.PunchWorkflow == nil || s.IDs == nil {
		return PunchReceipt{}, ErrUnavailable
	}
	if !requireNonEmpty(req.ClaimedWorkerRef, req.AssignmentRef, req.IdempotencyKey, req.DeviceKind) || req.DeviceTime.IsZero() {
		return PunchReceipt{}, reject(ErrInvalidRequest, "request", "", "claimed worker, assignment, source, idempotency key and device time are required")
	}
	tenant := tenantOf(p)
	now := s.now()
	if now.IsZero() || p.IssuedAt().After(now) || !p.ExpiresAt().After(now) {
		return PunchReceipt{}, ErrInvalidPrincipal
	}

	workerRef, projectRef, delegated, err := s.resolveWorkerAndAssignment(ctx, p, tenant, req.ClaimedWorkerRef, req.AssignmentRef)
	if err != nil {
		return PunchReceipt{}, err
	}
	canonicalDigest := punchInputDigest(tenant, workerRef, p.Subject(), delegated, kind, req)
	if prior, found, lookupErr := s.findPunchObservation(ctx, tenant, workerRef, req.DeviceKind, req.IdempotencyKey); lookupErr != nil {
		return PunchReceipt{}, lookupErr
	} else if found {
		if prior.Digest != canonicalDigest {
			return PunchReceipt{}, ErrPunchIdempotencyConflict
		}
		return s.replayPunch(ctx, tenant, workerRef, p.Subject(), projectRef, kind, req, prior)
	}

	source := timesession.PunchSource{Kind: req.DeviceKind, DeviceRef: req.DeviceRef, Method: req.Method}

	var base timesession.Session
	var expectedRevision uint64
	isNew := kind == timesession.PunchIn
	var sessionID string
	if isNew {
		sessionID = s.IDs.Deterministic(tenant, workerRef, req.AssignmentRef, "session-in", req.IdempotencyKey)
	} else {
		existing, err := s.Sessions.CurrentSession(ctx, tenant, workerRef, req.AssignmentRef)
		if err != nil {
			return PunchReceipt{}, err
		}
		base, err = unmarshalSession(existing.Payload)
		if err != nil {
			return PunchReceipt{}, err
		}
		sessionID = existing.ID
		expectedRevision = base.Revision
	}

	tp := timesession.Punch{
		Kind: kind, Tenant: tenant, Worker: workerRef, Actor: p.Subject(), Delegated: delegated,
		Assignment: req.AssignmentRef, SessionID: sessionID, IdempotencyKey: req.IdempotencyKey,
		DeviceTime: req.DeviceTime, ServerReceiptTime: now, Source: source,
		JobRef: req.JobRef, CostCodeRef: req.CostCodeRef, Travel: req.Travel,
		Scheduled: req.Scheduled, ExpectedRevision: expectedRevision,
	}

	_, outcome, applyErr := timesession.Apply(base, tp)
	if applyErr != nil && errors.Is(applyErr, timesession.ErrDuplicatePunch) {
		// A domain replay alone cannot prove durable workflow acceptance.
		return PunchReceipt{}, ErrUnavailable
	}
	if applyErr != nil {
		return PunchReceipt{}, applyErr
	}

	payload, err := marshalSession(outcome.Session)
	if err != nil {
		return PunchReceipt{}, err
	}
	digest := canonicalDigest

	record := SessionRecord{
		ID: sessionID, TenantID: tenant, WorkerRef: workerRef, AssignmentRef: req.AssignmentRef,
		Status: string(outcome.Session.State), Source: req.DeviceKind, ProjectRef: projectRef,
		Revision: outcome.Session.Revision, Payload: payload,
	}
	if len(outcome.Session.Segments) > 0 {
		record.OpenedAt = outcome.Session.Segments[0].Start
	}
	if outcome.Session.State == timesession.StateClosed || outcome.Session.State == timesession.StateAutoClosed {
		if n := len(outcome.Session.Segments); n > 0 {
			record.ClosedAt = outcome.Session.Segments[n-1].End
		}
	}
	event := SessionEvent{Kind: string(outcome.Kind), ActorRef: p.Subject(), IdempotencyKey: req.IdempotencyKey, Digest: digest, Payload: payload}

	obsID := s.IDs.Deterministic(tenant, "observation", req.DeviceKind, req.IdempotencyKey)
	obs := ObservationRecord{
		ID: obsID, TenantID: tenant, WorkerRef: workerRef, AssignmentRef: req.AssignmentRef,
		DeviceRef: req.DeviceRef, Source: req.DeviceKind, EventType: string(kind), ProjectRef: projectRef,
		IdempotencyKey: req.IdempotencyKey, Digest: digest, OccurredAt: req.DeviceTime, ReceivedAt: now, Payload: payload,
	}

	work := PunchWork{Session: record, SessionIsNew: isNew, ExpectedRevision: expectedRevision, ExpectedProjectionRevision: req.ExpectedProjectionRevision, SessionEvents: []SessionEvent{event}, Observation: obs}
	workflowResult, err := s.PunchWorkflow.ExecutePunch(ctx, tenant, work)
	if err != nil {
		if errors.Is(err, ErrRetryLater) {
			return PunchReceipt{Status: ReceiptRetryLater, WorkerRef: workerRef, AssignmentRef: req.AssignmentRef}, ErrRetryLater
		}
		return PunchReceipt{}, err
	}
	wantNode := "commit_punch"
	if kind == timesession.PunchOut && workflowResult.WorkflowID == "hcmnext.workflows.time.clock_in_out" {
		wantNode = "commit_clock_out"
	}
	if !workflowResult.Committed || workflowResult.InstanceID == [16]byte{} || !requireNonEmpty(workflowResult.WorkflowID, workflowResult.PlanDigest, workflowResult.StartKey, workflowResult.TraceID) || workflowResult.NodeID != wantNode || workflowResult.Attempt < 1 || workflowResult.InstanceVersion < 1 {
		return PunchReceipt{}, ErrUnavailable
	}
	result := workflowResult.PunchResult
	if result.Session.ID != work.Session.ID || result.Session.TenantID != tenant || result.Observation.ID != work.Observation.ID || result.Observation.TenantID != tenant {
		return PunchReceipt{}, ErrUnavailable
	}
	status := ReceiptAccepted
	if result.Duplicate {
		status = ReceiptDuplicate
	}
	receipt := receiptFromOutcome(outcome, workerRef, req.AssignmentRef, status, result.Observation.ID)
	receipt.WorkflowInstanceRef = workflowResult.InstanceID.String()
	receipt.WorkflowID = workflowResult.WorkflowID
	receipt.WorkflowPlanDigest = workflowResult.PlanDigest
	receipt.WorkflowStartKey = workflowResult.StartKey
	receipt.WorkflowTraceID = workflowResult.TraceID
	receipt.WorkflowNodeID = workflowResult.NodeID
	receipt.WorkflowAttempt = workflowResult.Attempt
	receipt.WorkflowInstanceVersion = workflowResult.InstanceVersion

	return receipt, nil
}

// findPunchObservation searches only the already-authorized worker and
// tenant scope. The store's cursor is opaque, so recovery never guesses an
// observation ID from client input and never discloses a foreign tenant row.
func (s Service) findPunchObservation(ctx context.Context, tenant, worker, source, key string) (ObservationRecord, bool, error) {
	if s.Observations == nil {
		return ObservationRecord{}, false, ErrUnavailable
	}
	if direct, ok := s.Observations.(PunchObservationLookup); ok {
		return direct.LookupPunchObservation(ctx, tenant, source, key)
	}
	cursor := ""
	for page := 0; page < 100; page++ {
		from := s.now().Add(-48 * time.Hour)
		to := s.now().Add(48 * time.Hour)
		rows, next, err := s.Observations.ListObservations(ctx, tenant, worker, from, to, cursor, 500)
		if err != nil {
			return ObservationRecord{}, false, err
		}
		for _, row := range rows {
			if row.Source == source && row.IdempotencyKey == key {
				return row, true, nil
			}
		}
		if next == "" || next == cursor {
			return ObservationRecord{}, false, nil
		}
		cursor = next
	}
	return ObservationRecord{}, false, ErrUnavailable
}

func (s Service) replayPunch(ctx context.Context, tenant, worker, actor, project string, kind timesession.PunchKind, req PunchRequest, prior ObservationRecord) (PunchReceipt, error) {
	resultSession, err := unmarshalSession(prior.Payload)
	if err != nil {
		return PunchReceipt{}, err
	}
	if resultSession.Tenant != tenant || resultSession.Worker != worker || resultSession.Assignment != req.AssignmentRef || resultSession.SessionID == "" || prior.TenantID != tenant || prior.WorkerRef != worker || prior.AssignmentRef != req.AssignmentRef {
		return PunchReceipt{}, ErrUnavailable
	}
	payload, err := marshalSession(resultSession)
	if err != nil {
		return PunchReceipt{}, err
	}
	record := SessionRecord{ID: resultSession.SessionID, TenantID: tenant, WorkerRef: worker, AssignmentRef: req.AssignmentRef, Status: string(resultSession.State), Source: prior.Source, ProjectRef: project, Revision: resultSession.Revision, Payload: payload}
	if len(resultSession.Segments) > 0 {
		record.OpenedAt = resultSession.Segments[0].Start
		record.ClosedAt = resultSession.Segments[len(resultSession.Segments)-1].End
	}
	work := PunchWork{Session: record, SessionIsNew: kind == timesession.PunchIn, ExpectedProjectionRevision: req.ExpectedProjectionRevision, Observation: prior, SessionEvents: []SessionEvent{{Kind: prior.EventType, ActorRef: actor, IdempotencyKey: prior.IdempotencyKey, Digest: prior.Digest, Payload: payload}}}
	workflowResult, err := s.PunchWorkflow.ExecutePunch(ctx, tenant, work)
	if err != nil {
		return PunchReceipt{}, err
	}
	result := workflowResult.PunchResult
	if !validReplayWorkflowResult(workflowResult, kind) || result.Session.TenantID != tenant || result.Session.ID != record.ID || result.Session.WorkerRef != worker || result.Session.AssignmentRef != req.AssignmentRef || result.Observation.TenantID != tenant || result.Observation.ID != prior.ID || result.Observation.Digest != prior.Digest {
		return PunchReceipt{}, ErrUnavailable
	}
	outcome := timesession.Outcome{Session: resultSession, Kind: replayOutcomeKind(kind)}
	receipt := receiptFromOutcome(outcome, worker, req.AssignmentRef, ReceiptDuplicate, prior.ID)
	receipt.WorkflowInstanceRef = workflowResult.InstanceID.String()
	receipt.WorkflowID = workflowResult.WorkflowID
	receipt.WorkflowPlanDigest = workflowResult.PlanDigest
	receipt.WorkflowStartKey = workflowResult.StartKey
	receipt.WorkflowTraceID = workflowResult.TraceID
	receipt.WorkflowNodeID = workflowResult.NodeID
	receipt.WorkflowAttempt = workflowResult.Attempt
	receipt.WorkflowInstanceVersion = workflowResult.InstanceVersion
	return receipt, nil
}

func validReplayWorkflowResult(result WorkflowPunchResult, kind timesession.PunchKind) bool {
	wantNode := "commit_punch"
	if kind == timesession.PunchOut && result.WorkflowID == "hcmnext.workflows.time.clock_in_out" {
		wantNode = "commit_clock_out"
	}
	return result.Committed && result.InstanceID != [16]byte{} && requireNonEmpty(result.WorkflowID, result.PlanDigest, result.StartKey, result.TraceID) && result.NodeID == wantNode && result.Attempt > 0 && result.InstanceVersion > 0
}

func replayOutcomeKind(kind timesession.PunchKind) timesession.OutcomeKind {
	switch kind {
	case timesession.PunchIn:
		return timesession.OutcomeOpened
	case timesession.PunchOut:
		return timesession.OutcomeClosed
	case timesession.PunchBreakStart, timesession.PunchMealStart:
		return timesession.OutcomeOnBreak
	case timesession.PunchBreakEnd, timesession.PunchMealEnd:
		return timesession.OutcomeResumed
	case timesession.PunchTransfer:
		return timesession.OutcomeTransferred
	default:
		return ""
	}
}

func receiptFromOutcome(outcome timesession.Outcome, workerRef, assignmentRef string, status ReceiptStatus, observationID string) PunchReceipt {
	return PunchReceipt{
		Status: status, SessionID: outcome.Session.SessionID, WorkerRef: workerRef, AssignmentRef: assignmentRef,
		State: outcome.Session.State, Revision: outcome.Session.Revision, Outcome: outcome.Kind,
		Exception: outcome.Exception, ObservationID: observationID,
	}
}

func marshalSession(s timesession.Session) ([]byte, error) { return json.Marshal(s) }

func unmarshalSession(b []byte) (timesession.Session, error) {
	var s timesession.Session
	if len(b) == 0 {
		return s, reject(ErrInvalidRequest, "session.payload", "", "session payload is empty")
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return timesession.Session{}, err
	}
	return s, nil
}

func punchDigest(parts ...string) string {
	sum := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(sum, "%d:%s;", len(p), p)
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

func punchInputDigest(tenant, worker, actor string, delegated bool, kind timesession.PunchKind, req PunchRequest) string {
	parts := []string{"hcmnext.clock.punch/v2", tenant, worker, actor, fmt.Sprint(delegated), string(kind), req.AssignmentRef, req.JobRef, req.CostCodeRef, fmt.Sprint(req.Travel), req.DeviceKind, req.DeviceRef, string(req.Method), req.DeviceTime.UTC().Format(time.RFC3339Nano), fmt.Sprint(req.Scheduled), req.IdempotencyKey}
	if req.ExpectedProjectionRevision != 0 {
		parts[0] = "hcmnext.clock.punch/v3"
		// Self-clock capture time is supplied by the server on each HTTP retry.
		// Its identity is the action, actor, assignment, key and pinned revision.
		if req.DeviceKind == "WEB" && !delegated {
			parts[13] = "server-capture"
		}
		parts = append(parts, fmt.Sprint(req.ExpectedProjectionRevision))
	}
	return punchDigest(parts...)
}
