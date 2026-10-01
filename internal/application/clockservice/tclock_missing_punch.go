package clockservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockrepair"
)

// MissingPunchWorkflowID is the published workflow definition for this
// capability. The executor, rather than this package, owns its nodes.
const MissingPunchWorkflowID = "hcmnext.workflows.time.fix_missing_punch"

var (
	ErrMissingPunchUnavailable = errors.New("clockservice: missing punch capability unavailable")
	ErrMissingPunchForbidden   = errors.New("clockservice: missing punch action forbidden")
	ErrMissingPunchConflict    = errors.New("clockservice: missing punch revision conflict")
	ErrMissingPunchClosed      = errors.New("clockservice: closed period requires typed reopen")
	ErrMissingPunchInvalid     = errors.New("clockservice: invalid missing punch")
)

// MissingPunchSession is the authoritative session facts needed to admit a
// request. MissingOut must be an existing exception; this service never
// invents one from a client claim.
type MissingPunchSession struct {
	TenantID, ID, WorkerRef     string
	Status                      string
	Revision                    uint64
	MissingOut                  bool
	PeriodClosed                bool
	OriginalWorkflowInstanceRef string
}

// MissingPunchObservation is the immutable original observation bound to a
// request. It is returned by the observation reader and never modified.
type MissingPunchObservation struct {
	ID          string
	TenantID    string
	Observation clock.TimeObservation
}

// MissingPunchRecord is the durable workflow projection returned by the
// executor. Workflow lineage is mandatory evidence for every state change.
type MissingPunchRecord struct {
	ID, TenantID, WorkerRef, SessionID, OriginalObservationID string
	ClaimedOutAt, CreatedAt, DecidedAt                        time.Time
	Reason, RequestedBy, DecidedBy, Decision, ReopenRef       string
	CorrectionObservationID, CorrectionDigest                 string
	ExpectedSessionRevision                                   uint64
	Revision                                                  uint64
	PeriodClosed                                              bool
	WorkflowInstanceID, WorkflowTraceID, WorkflowNodeID       string
	WorkflowPlanDigest                                        string
	OriginalWorkflowInstanceRef                               string
	WorkflowAttempt                                           int
	WorkflowInstanceVersion                                   int64
}

// MissingPunchRequest is a worker's proposed OUT, still awaiting review.
type MissingPunchRequest struct {
	SessionID, WorkerRef, OriginalObservationID string
	ProposedOutAt, Now                          time.Time
	Reason, IdempotencyKey                      string
	ExpectedRevision                            uint64
	PeriodClosed                                bool
}

// MissingPunchDecision is a supervisor's independent decision.
type MissingPunchDecision struct {
	RequestID, WorkerRef, DecisionNote, IdempotencyKey string
	ExpectedRevision                                   uint64
	Approve                                            bool
	ReopenRef                                          string
	PeriodClosed                                       bool
}

// MissingPunchSessionReader reads the exact tenant-scoped session.
type MissingPunchSessionReader interface {
	GetMissingPunchSession(context.Context, string, string) (MissingPunchSession, error)
}

// MissingPunchObservationReader loads the original immutable observation.
type MissingPunchObservationReader interface {
	GetMissingPunchObservation(context.Context, string, string) (MissingPunchObservation, error)
}

// MissingPunchReviewReader reloads the authoritative request at decision time.
type MissingPunchReviewReader interface {
	GetMissingPunchRequest(context.Context, string, string) (MissingPunchRecord, error)
}

// MissingPunchAuthorization resolves current authority and scope before any
// workflow call. Implementations must enforce worker self-service and the
// separate supervisor/approver scope.
type MissingPunchAuthorization interface {
	AuthorizeRequest(context.Context, *trust.Principal, string, string) error
	AuthorizeDecision(context.Context, *trust.Principal, string, string) error
}

// MissingPunchWorkflowRequest is the canonical workflow input.
type MissingPunchWorkflowRequest struct {
	Action, TenantID, RequestID, SessionID, WorkerRef, OriginalObservationID string
	OriginalWorkflowInstanceRef                                              string
	ClaimedOutAt, At                                                         time.Time
	Reason, Actor, DecisionNote, ReopenRef, IdempotencyKey                   string
	ExpectedRevision                                                         uint64
	Approve, PeriodClosed                                                    bool
}

// MissingPunchWorkflowResult proves the workflow commit and carries durable
// request evidence. A result without these fields is never accepted.
type MissingPunchWorkflowResult struct {
	Record          MissingPunchRecord
	InstanceID      uuid.UUID
	WorkflowID      string
	PlanDigest      string
	TraceID         string
	NodeID          string
	Attempt         int
	InstanceVersion int64
	Committed       bool
}

// MissingPunchWorkflowExecutor is the only writer for request and decision
// workflow state. It must execute the published workflow through its durable
// engine and return only a committed node result.
type MissingPunchWorkflowExecutor interface {
	ExecuteMissingPunch(context.Context, MissingPunchWorkflowRequest) (MissingPunchWorkflowResult, error)
}

// MissingPunchService is the application capability for governed missing
// OUT requests. Clock is trusted server time supplied by composition.
type MissingPunchService struct {
	Sessions      MissingPunchSessionReader
	Observations  MissingPunchObservationReader
	Reviews       MissingPunchReviewReader
	Authorization MissingPunchAuthorization
	Workflow      MissingPunchWorkflowExecutor
	Clock         func() time.Time
}

// RequestMissingPunch validates and submits a worker request. No durable
// correction is possible on this path.
func (s MissingPunchService) RequestMissingPunch(ctx context.Context, p *trust.Principal, req MissingPunchRequest) (MissingPunchRecord, error) {
	if err := validPrincipal(p); err != nil {
		return MissingPunchRecord{}, err
	}
	if s.Sessions == nil || s.Observations == nil || s.Authorization == nil || s.Workflow == nil || s.Clock == nil {
		return MissingPunchRecord{}, ErrMissingPunchUnavailable
	}
	tenant := tenantOf(p)
	now := s.Clock().UTC()
	if now.IsZero() || p.IssuedAt().After(now) || !p.ExpiresAt().After(now) {
		return MissingPunchRecord{}, ErrInvalidPrincipal
	}
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.WorkerRef) == "" || strings.TrimSpace(req.OriginalObservationID) == "" || strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.ProposedOutAt.IsZero() || req.ExpectedRevision == 0 {
		return MissingPunchRecord{}, ErrMissingPunchInvalid
	}
	if err := s.Authorization.AuthorizeRequest(ctx, p, tenant, req.WorkerRef); err != nil {
		return MissingPunchRecord{}, errors.Join(ErrMissingPunchForbidden, err)
	}
	session, err := s.Sessions.GetMissingPunchSession(ctx, tenant, req.SessionID)
	if err != nil {
		return MissingPunchRecord{}, err
	}
	if session.TenantID != tenant || session.ID != req.SessionID || session.WorkerRef != req.WorkerRef || session.Status != "OPEN" || !session.MissingOut || !validMissingPunchInstance(session.OriginalWorkflowInstanceRef) {
		return MissingPunchRecord{}, ErrMissingPunchInvalid
	}
	if session.Revision != req.ExpectedRevision {
		return MissingPunchRecord{}, ErrMissingPunchConflict
	}
	original, err := s.Observations.GetMissingPunchObservation(ctx, tenant, req.OriginalObservationID)
	if err != nil {
		return MissingPunchRecord{}, err
	}
	if original.TenantID != tenant || original.ID != req.OriginalObservationID || !original.Observation.Accepted || original.Observation.WorkerRef != req.WorkerRef || original.Observation.EventType != clock.EventClockIn || !req.ProposedOutAt.After(original.Observation.OccurredAt) {
		return MissingPunchRecord{}, ErrMissingPunchInvalid
	}
	if req.ProposedOutAt.After(now.Add(5 * time.Minute)) {
		return MissingPunchRecord{}, ErrMissingPunchInvalid
	}
	result, err := s.Workflow.ExecuteMissingPunch(ctx, MissingPunchWorkflowRequest{Action: "REQUEST", TenantID: tenant, SessionID: req.SessionID, WorkerRef: req.WorkerRef, OriginalObservationID: req.OriginalObservationID, OriginalWorkflowInstanceRef: session.OriginalWorkflowInstanceRef, ClaimedOutAt: req.ProposedOutAt.UTC(), At: now, Reason: strings.TrimSpace(req.Reason), Actor: p.Subject(), IdempotencyKey: req.IdempotencyKey, ExpectedRevision: req.ExpectedRevision, PeriodClosed: session.PeriodClosed})
	if err != nil {
		return MissingPunchRecord{}, err
	}
	if err := validateMissingPunchCommit(result, tenant, "REQUEST"); err != nil {
		return MissingPunchRecord{}, err
	}
	if result.Record.SessionID != req.SessionID || result.Record.WorkerRef != req.WorkerRef || result.Record.OriginalObservationID != req.OriginalObservationID || !result.Record.ClaimedOutAt.Equal(req.ProposedOutAt.UTC()) || result.Record.Reason != strings.TrimSpace(req.Reason) || result.Record.RequestedBy != p.Subject() || result.Record.Revision != 1 || result.Record.ExpectedSessionRevision != req.ExpectedRevision || result.Record.ID == "" || result.Record.CreatedAt.IsZero() || result.Record.CreatedAt.After(now) || result.Record.Decision != "PENDING" || result.Record.PeriodClosed != session.PeriodClosed || result.Record.OriginalWorkflowInstanceRef != session.OriginalWorkflowInstanceRef {
		return MissingPunchRecord{}, ErrMissingPunchConflict
	}
	return result.Record, nil
}

// DecideMissingPunch approves or rejects a request. Approval executes the
// workflow, whose governed correction node owns the append and its lineage.
func (s MissingPunchService) DecideMissingPunch(ctx context.Context, p *trust.Principal, req MissingPunchDecision) (MissingPunchRecord, error) {
	if err := validPrincipal(p); err != nil {
		return MissingPunchRecord{}, err
	}
	if s.Workflow == nil || s.Authorization == nil || s.Observations == nil || s.Reviews == nil || s.Clock == nil {
		return MissingPunchRecord{}, ErrMissingPunchUnavailable
	}
	tenant := tenantOf(p)
	if strings.TrimSpace(req.RequestID) == "" || strings.TrimSpace(req.WorkerRef) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.ExpectedRevision == 0 {
		return MissingPunchRecord{}, ErrMissingPunchInvalid
	}
	if p.Subject() == req.WorkerRef {
		return MissingPunchRecord{}, ErrSelfApproval
	}
	now := s.Clock().UTC()
	if now.IsZero() || p.IssuedAt().After(now) || !p.ExpiresAt().After(now) {
		return MissingPunchRecord{}, ErrInvalidPrincipal
	}
	if err := s.Authorization.AuthorizeDecision(ctx, p, tenant, req.WorkerRef); err != nil {
		return MissingPunchRecord{}, errors.Join(ErrMissingPunchForbidden, err)
	}
	current, err := s.Reviews.GetMissingPunchRequest(ctx, tenant, req.RequestID)
	if err != nil {
		return MissingPunchRecord{}, err
	}
	if current.TenantID != tenant || current.ID != req.RequestID || current.WorkerRef != req.WorkerRef || current.Revision != req.ExpectedRevision {
		return MissingPunchRecord{}, ErrMissingPunchConflict
	}
	if current.RequestedBy == p.Subject() {
		return MissingPunchRecord{}, ErrSelfApproval
	}
	if current.PeriodClosed && strings.TrimSpace(req.ReopenRef) == "" {
		return MissingPunchRecord{}, ErrMissingPunchClosed
	}
	if current.Decision != "PENDING" || current.SessionID == "" || current.OriginalObservationID == "" || !validMissingPunchInstance(current.OriginalWorkflowInstanceRef) {
		return MissingPunchRecord{}, ErrMissingPunchConflict
	}
	if !req.Approve && strings.TrimSpace(req.DecisionNote) == "" {
		return MissingPunchRecord{}, ErrMissingPunchInvalid
	}
	result, err := s.Workflow.ExecuteMissingPunch(ctx, MissingPunchWorkflowRequest{Action: "DECIDE", TenantID: tenant, RequestID: req.RequestID, WorkerRef: req.WorkerRef, SessionID: current.SessionID, OriginalObservationID: current.OriginalObservationID, OriginalWorkflowInstanceRef: current.OriginalWorkflowInstanceRef, ClaimedOutAt: current.ClaimedOutAt, Reason: current.Reason, Actor: p.Subject(), DecisionNote: strings.TrimSpace(req.DecisionNote), ReopenRef: strings.TrimSpace(req.ReopenRef), IdempotencyKey: req.IdempotencyKey, ExpectedRevision: req.ExpectedRevision, Approve: req.Approve, PeriodClosed: current.PeriodClosed, At: now})
	if err != nil {
		return MissingPunchRecord{}, err
	}
	if err := validateMissingPunchCommit(result, tenant, "DECIDE"); err != nil {
		return MissingPunchRecord{}, err
	}
	wantNode := clockrepair.NodeApproval
	if req.Approve {
		wantNode = clockrepair.NodeCorrection
	}
	if result.NodeID != wantNode {
		return MissingPunchRecord{}, ErrMissingPunchUnavailable
	}
	if result.Record.WorkerRef != req.WorkerRef || result.Record.ID != req.RequestID || result.Record.Revision != req.ExpectedRevision+1 || !result.Record.ClaimedOutAt.Equal(current.ClaimedOutAt) || result.Record.Reason != current.Reason || result.Record.OriginalObservationID != current.OriginalObservationID {
		return MissingPunchRecord{}, ErrMissingPunchConflict
	}
	wantDecision := "REJECTED"
	if req.Approve {
		wantDecision = "APPROVED"
	}
	if result.Record.SessionID != current.SessionID || result.Record.DecidedBy != p.Subject() || result.Record.Decision != wantDecision || result.Record.ReopenRef != strings.TrimSpace(req.ReopenRef) || result.Record.PeriodClosed != current.PeriodClosed || result.Record.OriginalWorkflowInstanceRef != current.OriginalWorkflowInstanceRef {
		return MissingPunchRecord{}, ErrMissingPunchConflict
	}
	return result.Record, nil
}

func validateMissingPunchCommit(result MissingPunchWorkflowResult, tenant, action string) error {
	if !result.Committed || result.InstanceID == uuid.Nil || result.WorkflowID != MissingPunchWorkflowID || result.TraceID == "" || result.NodeID == "" || result.Attempt < 1 || result.InstanceVersion < 1 || result.Record.TenantID != tenant {
		return ErrMissingPunchUnavailable
	}
	if result.PlanDigest == "" || result.Record.WorkflowPlanDigest != result.PlanDigest || result.Record.WorkflowInstanceID != result.InstanceID.String() || result.Record.WorkflowTraceID != result.TraceID || result.Record.WorkflowNodeID != result.NodeID || result.Record.WorkflowAttempt != result.Attempt || result.Record.WorkflowInstanceVersion != result.InstanceVersion {
		return ErrMissingPunchUnavailable
	}
	if action == "REQUEST" && result.NodeID != clockrepair.NodeCommitRequest {
		return ErrMissingPunchUnavailable
	}
	if action == "DECIDE" && result.NodeID == "" {
		return ErrMissingPunchUnavailable
	}
	return nil
}

func validMissingPunchInstance(value string) bool {
	instance, err := uuid.Parse(value)
	return err == nil && instance != uuid.Nil
}
