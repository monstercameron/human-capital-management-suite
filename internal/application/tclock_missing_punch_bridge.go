package application

import (
	"context"
	"strings"

	"github.com/google/uuid"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	transporttimeclock "github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// MissingPunchSessionFacts are server-owned facts used to construct a
// request. A resolver must authorize the principal before returning them.
type MissingPunchSessionFacts struct {
	WorkerRef             string
	OriginalObservationID string
	ExpectedRevision      uint64
	PeriodClosed          bool
}

// MissingPunchRequestFacts are server-owned facts used to make a decision.
// WorkerRef is never accepted from the transport caller.
type MissingPunchRequestFacts struct {
	WorkerRef string
}

// MissingPunchResolver resolves scoped facts after principal authorization.
// Implementations must avoid existence leaks by returning a typed forbidden
// or not-found error before exposing facts outside the principal's scope.
type MissingPunchResolver interface {
	ResolveMissingPunchSession(context.Context, *trust.Principal, string) (MissingPunchSessionFacts, error)
	ResolveMissingPunchRequest(context.Context, *trust.Principal, string) (MissingPunchRequestFacts, error)
}

// MissingPunchBridge adapts the governed clock service to the time-clock
// transport application port. It performs no workflow execution itself.
type MissingPunchBridge struct {
	Service           clockservice.MissingPunchService
	Resolver          MissingPunchResolver
	ReadStore         MissingPunchReadStore
	ReadAuthorization MissingPunchReadAuthorization
}

// NewMissingPunchBridge constructs a bridge. Missing dependencies remain an
// unavailable capability and are reported as such at call time.
func NewMissingPunchBridge(service clockservice.MissingPunchService, resolver MissingPunchResolver) *MissingPunchBridge {
	return &MissingPunchBridge{Service: service, Resolver: resolver}
}

// NewMissingPunchBridgeWithReads constructs a bridge with the optional
// server-scoped read projection wired. Commands remain independently usable
// when read dependencies are omitted from NewMissingPunchBridge.
func NewMissingPunchBridgeWithReads(service clockservice.MissingPunchService, resolver MissingPunchResolver, store MissingPunchReadStore, authorization MissingPunchReadAuthorization) *MissingPunchBridge {
	return &MissingPunchBridge{Service: service, Resolver: resolver, ReadStore: store, ReadAuthorization: authorization}
}

// SubmitCorrection derives worker, original-observation and revision facts
// from the trusted server resolver before invoking the application service.
func (b *MissingPunchBridge) SubmitCorrection(ctx context.Context, p *trust.Principal, in transporttimeclock.MissingPunchSubmit) (transporttimeclock.MissingPunchCorrection, error) {
	if b == nil || b.Resolver == nil || missingPunchServiceUnavailable(b.Service) {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrMissingPunchUnavailable
	}
	if p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrInvalidPrincipal
	}
	if strings.TrimSpace(in.SessionID) == "" || strings.TrimSpace(in.Reason) == "" || strings.TrimSpace(in.IdempotencyKey) == "" || in.ClaimedOccurredAt.IsZero() || in.ExpectedRevision == 0 {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrMissingPunchInvalid
	}
	if in.ClaimedEventType != "" && in.ClaimedEventType != "CLOCK_OUT" {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrMissingPunchInvalid
	}
	facts, err := b.Resolver.ResolveMissingPunchSession(ctx, p, in.SessionID)
	if err != nil {
		return transporttimeclock.MissingPunchCorrection{}, err
	}
	if facts.WorkerRef == "" || facts.OriginalObservationID == "" || facts.ExpectedRevision == 0 {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrMissingPunchUnavailable
	}
	record, err := b.Service.RequestMissingPunch(ctx, p, clockservice.MissingPunchRequest{
		SessionID: in.SessionID, WorkerRef: facts.WorkerRef, OriginalObservationID: facts.OriginalObservationID,
		ProposedOutAt: in.ClaimedOccurredAt, Reason: in.Reason, IdempotencyKey: in.IdempotencyKey,
		ExpectedRevision: in.ExpectedRevision, PeriodClosed: facts.PeriodClosed,
	})
	if err != nil {
		return transporttimeclock.MissingPunchCorrection{}, err
	}
	return missingPunchCorrection(record, "PENDING", in.Reason)
}

// ReviewCorrection derives the affected worker from the trusted resolver and
// delegates authorization, stale checks and execution to the service.
func (b *MissingPunchBridge) ReviewCorrection(ctx context.Context, p *trust.Principal, in transporttimeclock.MissingPunchReview) (transporttimeclock.MissingPunchCorrection, error) {
	if b == nil || b.Resolver == nil || missingPunchServiceUnavailable(b.Service) {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrMissingPunchUnavailable
	}
	if p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrInvalidPrincipal
	}
	if strings.TrimSpace(in.RequestID) == "" || strings.TrimSpace(in.Reason) == "" || strings.TrimSpace(in.IdempotencyKey) == "" || in.ExpectedRevision == 0 {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrMissingPunchInvalid
	}
	if in.Decision != "APPROVED" && in.Decision != "REJECTED" {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrMissingPunchInvalid
	}
	facts, err := b.Resolver.ResolveMissingPunchRequest(ctx, p, in.RequestID)
	if err != nil {
		return transporttimeclock.MissingPunchCorrection{}, err
	}
	if facts.WorkerRef == "" {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrMissingPunchUnavailable
	}
	record, err := b.Service.DecideMissingPunch(ctx, p, clockservice.MissingPunchDecision{
		RequestID: in.RequestID, WorkerRef: facts.WorkerRef, DecisionNote: in.Reason,
		IdempotencyKey: in.IdempotencyKey, ExpectedRevision: in.ExpectedRevision, Approve: in.Decision == "APPROVED",
	})
	if err != nil {
		return transporttimeclock.MissingPunchCorrection{}, err
	}
	return missingPunchCorrection(record, in.Decision, in.Reason)
}

func missingPunchCorrection(record clockservice.MissingPunchRecord, statusValue, reason string) (transporttimeclock.MissingPunchCorrection, error) {
	if record.ID == "" || record.TenantID == "" || record.WorkerRef == "" || record.SessionID == "" || record.ClaimedOutAt.IsZero() || record.Revision == 0 || uuid.Validate(record.OriginalWorkflowInstanceRef) != nil || uuid.Validate(record.WorkflowInstanceID) != nil || record.WorkflowTraceID == "" || record.WorkflowNodeID == "" || record.WorkflowPlanDigest == "" || record.WorkflowAttempt < 1 || record.WorkflowInstanceVersion < 1 {
		return transporttimeclock.MissingPunchCorrection{}, clockservice.ErrMissingPunchUnavailable
	}
	return transporttimeclock.MissingPunchCorrection{
		RequestID: record.ID, WorkerRef: record.WorkerRef, SessionID: record.SessionID,
		ClaimedEventType: "CLOCK_OUT", ClaimedOccurredAt: record.ClaimedOutAt,
		RequestReason: record.Reason, Status: statusValue, DecisionReason: reason, Revision: record.Revision,
		WorkflowReceipt: transporttimeclock.MissingPunchReceipt{
			ReceiptID: record.ID, WorkflowInstanceRef: record.WorkflowInstanceID, WorkflowTraceID: record.WorkflowTraceID,
			WorkflowNodeID: record.WorkflowNodeID, WorkflowID: clockservice.MissingPunchWorkflowID,
			WorkflowPlanDigest: record.WorkflowPlanDigest, WorkflowAttempt: int32(record.WorkflowAttempt), WorkflowInstanceVersion: record.WorkflowInstanceVersion,
		},
	}, nil
}

func missingPunchServiceUnavailable(s clockservice.MissingPunchService) bool {
	return s.Sessions == nil || s.Observations == nil || s.Reviews == nil || s.Authorization == nil || s.Workflow == nil || s.Clock == nil
}

var _ transporttimeclock.MissingPunchApplication = (*MissingPunchBridge)(nil)
