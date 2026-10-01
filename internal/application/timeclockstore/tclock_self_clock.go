package timeclockstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// SelfWorkerSource is the authoritative workforce read used by browser self
// clock. Implementations must be tenant-scoped current reads.
type SelfWorkerSource interface {
	ResolveSelfWorker(context.Context, string, string) (clockservice.SelfWorker, error)
}

// SelfProfileSource resolves the currently effective published time profile.
type SelfProfileSource interface {
	ResolvePublishedProfile(context.Context, string, string, string, time.Time) (timeprofile.TimeProfile, error)
}

// SelfProjectionSource reads the localized upstream clock projection.
type SelfProjectionSource interface {
	ReadSelfClock(context.Context, string, string, string) (clockservice.SelfClockStatus, error)
}

// WorkerResolver adapts the authoritative workforce reader to the clock port.
type WorkerResolver struct{ Source SelfWorkerSource }

func (r WorkerResolver) ResolveSelfWorker(ctx context.Context, tenant, subject string) (clockservice.SelfWorker, error) {
	if r.Source == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(subject) == "" {
		return clockservice.SelfWorker{}, clockservice.ErrUnavailable
	}
	return r.Source.ResolveSelfWorker(ctx, tenant, subject)
}

// ProfileResolver adapts the published profile reader to the clock port.
type ProfileResolver struct{ Source SelfProfileSource }

func (r ProfileResolver) ResolvePublishedProfile(ctx context.Context, tenant, worker, assignment string, at time.Time) (timeprofile.TimeProfile, error) {
	if r.Source == nil {
		return timeprofile.TimeProfile{}, clockservice.ErrUnavailable
	}
	return r.Source.ResolvePublishedProfile(ctx, tenant, worker, assignment, at)
}

// ProjectionStore adapts the durable current-session projection reader.
type ProjectionStore struct{ Source SelfProjectionSource }

func (s ProjectionStore) ReadSelfClock(ctx context.Context, tenant, worker, assignment string) (clockservice.SelfClockStatus, error) {
	if s.Source == nil {
		return clockservice.SelfClockStatus{}, clockservice.ErrUnavailable
	}
	return s.Source.ReadSelfClock(ctx, tenant, worker, assignment)
}

// WorkflowActionExecutor routes self clock actions through the existing
// clockservice punch workflow. It never fabricates a receipt or bypasses the
// service's authoritative workflow gate.
type WorkflowActionExecutor struct {
	Service    clockservice.Service
	Clock      func() time.Time
	Projection clockservice.SelfClockStore
}

type selfClockPuncher interface {
	ClockIn(context.Context, *trust.Principal, clockservice.PunchRequest) (clockservice.PunchReceipt, error)
	ClockOut(context.Context, *trust.Principal, clockservice.ClockOutRequest) (clockservice.PunchReceipt, error)
	StartBreak(context.Context, *trust.Principal, clockservice.PunchRequest) (clockservice.PunchReceipt, error)
	EndBreak(context.Context, *trust.Principal, clockservice.PunchRequest) (clockservice.PunchReceipt, error)
}

func (e WorkflowActionExecutor) ExecutePublishedClockAction(ctx context.Context, p *trust.Principal, req clockservice.SelfClockActionRequest) (clockservice.SelfClockActionResult, error) {
	if e.Clock == nil || e.Service.PunchWorkflow == nil {
		return clockservice.SelfClockActionResult{}, clockservice.ErrUnavailable
	}
	at := e.Clock().UTC()
	if at.IsZero() {
		return clockservice.SelfClockActionResult{}, clockservice.ErrUnavailable
	}
	if e.Projection == nil || req.ExpectedRevision == 0 {
		return clockservice.SelfClockActionResult{}, clockservice.ErrUnavailable
	}
	punch := clockservice.PunchRequest{ClaimedWorkerRef: req.WorkerRef, AssignmentRef: req.AssignmentRef, DeviceKind: "WEB", DeviceTime: at, IdempotencyKey: req.IdempotencyKey, ExpectedProjectionRevision: req.ExpectedRevision}
	receipt, err := executeSelfClockPunch(e.Service, ctx, p, req.Action, punch)
	if err != nil {
		return clockservice.SelfClockActionResult{}, err
	}
	status, err := e.Projection.ReadSelfClock(ctx, string(p.Tenant()), req.WorkerRef, req.AssignmentRef)
	if err != nil {
		return clockservice.SelfClockActionResult{}, err
	}
	return clockservice.SelfClockActionResult{ReceiptID: receipt.ObservationID, WorkerRef: receipt.WorkerRef, AssignmentRef: receipt.AssignmentRef, WorkflowInstanceRef: receipt.WorkflowInstanceRef, PublishedPlanRef: receipt.WorkflowPlanDigest, WorkflowID: receipt.WorkflowID, WorkflowTraceID: receipt.WorkflowTraceID, WorkflowNodeID: receipt.WorkflowNodeID, WorkflowAttempt: receipt.WorkflowAttempt, WorkflowInstanceVersion: receipt.WorkflowInstanceVersion, Status: status}, nil
}

func executeSelfClockPunch(puncher selfClockPuncher, ctx context.Context, p *trust.Principal, action string, punch clockservice.PunchRequest) (clockservice.PunchReceipt, error) {
	var receipt clockservice.PunchReceipt
	var err error
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "OUT":
		receipt, err = puncher.ClockOut(ctx, p, clockservice.ClockOutRequest{PunchRequest: punch})
	case "START_BREAK":
		receipt, err = puncher.StartBreak(ctx, p, punch)
	case "END_BREAK":
		receipt, err = puncher.EndBreak(ctx, p, punch)
	case "IN":
		receipt, err = puncher.ClockIn(ctx, p, punch)
	default:
		return clockservice.PunchReceipt{}, clockservice.ErrInvalidRequest
	}
	return receipt, err
}

var _ clockservice.SelfWorkerResolver = WorkerResolver{}
var _ clockservice.PublishedEligibilityProfile = ProfileResolver{}
var _ clockservice.SelfClockStore = ProjectionStore{}
var _ clockservice.SelfClockActionExecutor = WorkflowActionExecutor{}
