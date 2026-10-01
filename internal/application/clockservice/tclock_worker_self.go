package clockservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// SelfWorkerResolver maps the trusted human subject to its canonical active
// worker and assignment. It never accepts a worker reference from the client.
type SelfWorkerResolver interface {
	ResolveSelfWorker(context.Context, string, string) (SelfWorker, error)
}

// SelfWorker is the server-resolved identity and published assignment for a
// worker's own clock. Display and schedule labels are upstream projections.
type SelfWorker struct {
	WorkerRef, DisplayName, AssignmentRef, ScheduleLabel string
	Active                                               bool
}

// PublishedEligibilityProfile resolves the assignment's effective published
// time profile. A self clock may execute only a valid PUNCH profile.
type PublishedEligibilityProfile interface {
	ResolvePublishedProfile(context.Context, string, string, string, time.Time) (timeprofile.TimeProfile, error)
}

// SelfClockStore reads the current worker-owned clock projection. Implementors
// read current session state and immutable observations in one tenant scope.
type SelfClockStore interface {
	ReadSelfClock(context.Context, string, string, string) (SelfClockStatus, error)
}

// SelfClockStatus is the upstream, localized presentation projection returned
// to the worker. The service does not derive labels or status text.
type SelfClockStatus struct {
	WorkerLabel    string `json:"worker_label"`
	ScheduleLabel  string `json:"schedule_label"`
	StatusLabel    string `json:"status_label"`
	LastEventLabel string `json:"last_event_label"`
	// StatusCode is the server-owned position in the work session (CLOCKED_IN,
	// CLOCKED_OUT or ON_BREAK). StatusLabel is localized copy; this is what a
	// client branches on to decide which actions the worker is offered.
	StatusCode string `json:"status_code,omitempty"`
	Revision   uint64 `json:"revision"`
}

// SelfClockActionExecutor hands a self action to the published workflow and
// intent runtime. It must not call the raw punch path or mutate a store.
type SelfClockActionExecutor interface {
	ExecutePublishedClockAction(context.Context, *trust.Principal, SelfClockActionRequest) (SelfClockActionResult, error)
}

// SelfClockActionRequest is pinned to the resolved worker, assignment,
// expected projection revision and caller idempotency key.
type SelfClockActionRequest struct {
	Action           string `json:"action"`
	WorkerRef        string `json:"worker_ref"`
	AssignmentRef    string `json:"assignment_ref"`
	IdempotencyKey   string `json:"idempotency_key"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

// SelfClockActionResult is the workflow runtime's receipt and new projection.
type SelfClockActionResult struct {
	ReceiptID               string          `json:"receipt_id"`
	WorkerRef               string          `json:"worker_ref"`
	AssignmentRef           string          `json:"assignment_ref"`
	WorkflowInstanceRef     string          `json:"workflow_instance_ref"`
	PublishedPlanRef        string          `json:"published_plan_ref"`
	WorkflowID              string          `json:"workflow_id"`
	WorkflowTraceID         string          `json:"workflow_trace_id"`
	WorkflowNodeID          string          `json:"workflow_node_id"`
	WorkflowAttempt         int             `json:"workflow_attempt"`
	WorkflowInstanceVersion int64           `json:"workflow_instance_version"`
	Status                  SelfClockStatus `json:"status"`
}

// WorkerSelfService composes the worker self-service boundary without adding
// transport credentials or a second clock store to the existing device flow.
type WorkerSelfService struct {
	Workers  SelfWorkerResolver
	Profiles PublishedEligibilityProfile
	Store    SelfClockStore
	Actions  SelfClockActionExecutor
	Clock    func() time.Time
}

// GetSelfClock resolves the trusted principal's own worker and reads its
// current authoritative projection.
func (s WorkerSelfService) GetSelfClock(ctx context.Context, p *trust.Principal) (SelfClockStatus, error) {
	worker, profile, err := s.resolve(ctx, p)
	if err != nil {
		return SelfClockStatus{}, err
	}
	if err := requirePunchProfile(profile); err != nil {
		return SelfClockStatus{}, err
	}
	status, err := s.Store.ReadSelfClock(ctx, string(p.Tenant()), worker.WorkerRef, worker.AssignmentRef)
	if err != nil {
		return SelfClockStatus{}, err
	}
	if strings.TrimSpace(status.WorkerLabel) == "" {
		status.WorkerLabel = worker.DisplayName
	}
	if strings.TrimSpace(status.ScheduleLabel) == "" {
		status.ScheduleLabel = worker.ScheduleLabel
	}
	if !completeSelfClockStatus(status) {
		return SelfClockStatus{}, fmt.Errorf("%w: self clock projection is incomplete", ErrUnavailable)
	}
	if status.Revision == 0 {
		return SelfClockStatus{}, fmt.Errorf("%w: self clock projection revision is missing", ErrUnavailable)
	}
	return status, nil
}

// ExecuteSelfClockAction submits an IN, OUT, START_BREAK or END_BREAK action to the published
// workflow executor after resolving identity and the current eligibility.
func (s WorkerSelfService) ExecuteSelfClockAction(ctx context.Context, p *trust.Principal, req SelfClockActionRequest) (SelfClockActionResult, error) {
	worker, profile, err := s.resolve(ctx, p)
	if err != nil {
		return SelfClockActionResult{}, err
	}
	if err := requirePunchProfile(profile); err != nil {
		return SelfClockActionResult{}, err
	}
	if !validSelfClockAction(req.Action) {
		return SelfClockActionResult{}, reject(ErrInvalidRequest, "action", req.Action, "unsupported self clock action")
	}
	if req.ExpectedRevision == 0 || strings.TrimSpace(req.IdempotencyKey) == "" {
		return SelfClockActionResult{}, reject(ErrInvalidRequest, "request", "", "expected revision and idempotency key are required")
	}
	if s.Actions == nil {
		return SelfClockActionResult{}, ErrUnavailable
	}
	// The workflow commit fences new writes against ExpectedRevision after
	// checking durable idempotency. A precheck here would reject valid retries.
	req.WorkerRef, req.AssignmentRef = worker.WorkerRef, worker.AssignmentRef
	result, err := s.Actions.ExecutePublishedClockAction(ctx, p, req)
	if err != nil {
		return SelfClockActionResult{}, err
	}
	if !validWorkflowReceipt(result, worker.WorkerRef, worker.AssignmentRef, req.Action) {
		return SelfClockActionResult{}, fmt.Errorf("%w: workflow action receipt is incomplete", ErrUnavailable)
	}
	return result, nil
}

func validSelfClockAction(action string) bool {
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "IN", "OUT", "START_BREAK", "END_BREAK":
		return true
	default:
		return false
	}
}

// requirePunchProfile refuses a profile that does not record time by clock and
// names why, so an exempt worker is told so instead of being sent to a
// supervisor for access they cannot be given.
func requirePunchProfile(profile timeprofile.TimeProfile) error {
	if profile.Capture == timeprofile.CapturePunch {
		return nil
	}
	if profile.Exemption == timeprofile.Exempt {
		return NotEligible(ReasonExempt, "published profile is exempt from time recording")
	}
	return NotEligible(ReasonCaptureNotPunch, "published profile does not record time by clock: "+string(profile.Capture))
}

func validWorkflowReceipt(result SelfClockActionResult, workerRef, assignmentRef, action string) bool {
	instance, err := uuid.Parse(result.WorkflowInstanceRef)
	wantNode := "commit_punch"
	if result.WorkflowID == "hcmnext.workflows.time.clock_in_out" {
		switch strings.ToUpper(strings.TrimSpace(action)) {
		case "OUT":
			wantNode = "commit_clock_out"
		}
	}
	if result.WorkflowID != "hcmnext.workflows.time.clock_in_out" || result.WorkflowNodeID != wantNode {
		return false
	}
	return result.ReceiptID != "" && result.WorkerRef == workerRef && result.AssignmentRef == assignmentRef &&
		err == nil && instance != uuid.Nil && instance.String() == strings.ToLower(result.WorkflowInstanceRef) && result.PublishedPlanRef != "" &&
		result.WorkflowTraceID != "" && result.WorkflowNodeID == wantNode && result.WorkflowAttempt > 0 &&
		result.WorkflowInstanceVersion > 0 && result.Status.Revision != 0
}

func (s WorkerSelfService) resolve(ctx context.Context, p *trust.Principal) (SelfWorker, timeprofile.TimeProfile, error) {
	if s.Clock == nil {
		return SelfWorker{}, timeprofile.TimeProfile{}, ErrUnavailable
	}
	now := s.Clock().UTC()
	if err := validHumanPrincipal(p, now); err != nil {
		return SelfWorker{}, timeprofile.TimeProfile{}, err
	}
	if s.Workers == nil || s.Profiles == nil || s.Store == nil {
		return SelfWorker{}, timeprofile.TimeProfile{}, ErrUnavailable
	}
	worker, err := s.Workers.ResolveSelfWorker(ctx, string(p.Tenant()), p.Subject())
	if err != nil {
		return SelfWorker{}, timeprofile.TimeProfile{}, err
	}
	if !worker.Active || strings.TrimSpace(worker.WorkerRef) == "" {
		return SelfWorker{}, timeprofile.TimeProfile{}, NotEligible(ReasonNoWorkerRecord, "principal does not resolve to an active worker")
	}
	if strings.TrimSpace(worker.AssignmentRef) == "" {
		return SelfWorker{}, timeprofile.TimeProfile{}, NotEligible(ReasonNoAssignment, "worker has no current assignment")
	}
	profile, err := s.Profiles.ResolvePublishedProfile(ctx, string(p.Tenant()), worker.WorkerRef, worker.AssignmentRef, now)
	if err != nil {
		return SelfWorker{}, timeprofile.TimeProfile{}, err
	}
	if err := profile.Validate(); err != nil {
		return SelfWorker{}, timeprofile.TimeProfile{}, fmt.Errorf("%w: published profile: %v", ErrWorkerNotEligible, err)
	}
	return worker, profile, nil
}

func validHumanPrincipal(p *trust.Principal, now time.Time) error {
	if err := validPrincipal(p); err != nil {
		return err
	}
	if p.SubjectKind() != trust.SubjectKindHuman || now.IsZero() || p.IssuedAt().IsZero() || p.ExpiresAt().IsZero() || now.Before(p.IssuedAt()) || !now.Before(p.ExpiresAt()) {
		return ErrInvalidPrincipal
	}
	return nil
}

func completeSelfClockStatus(status SelfClockStatus) bool {
	return strings.TrimSpace(status.WorkerLabel) != "" && strings.TrimSpace(status.ScheduleLabel) != "" && strings.TrimSpace(status.StatusLabel) != "" && strings.TrimSpace(status.LastEventLabel) != ""
}
