package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var (
	// ErrCancellationAmbiguous means an irreversible effect has run without a
	// durable observation that proves it is safe to cancel.
	ErrCancellationAmbiguous = errors.New("workflow execute: cancellation requires effect observation")
	// ErrCancellationTerminal means cancellation cannot rewrite a terminal
	// instance's history.
	ErrCancellationTerminal = errors.New("workflow execute: terminal instance cannot be cancelled")
)

// CancellationRequest is the caller-held version and plan context for a
// governed cancellation. The plan is required so execute can distinguish a
// business effect from pure, approval, and wait nodes.
type CancellationRequest struct {
	TenantID                uuid.UUID
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	Plan                    *workflow.CompiledWorkflow
	Reason                  string
	RequestedBy             string
	RecordedAt              time.Time
}

// CancellationResult reports the durable cancelled instance.
type CancellationResult struct {
	Instance runtime.Instance
}

// Cancel moves a live instance through CANCELLING to CANCELLED at one safe
// point. It refuses after any declared write effect has succeeded or remains
// active, because an external or internal effect may already be live. Such a
// request must first be observed or repaired; it never claims a clean cancel.
func (d *Driver) Cancel(ctx context.Context, req CancellationRequest) (ret0 CancellationResult, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.cancel", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.ExpectedInstanceVersion < 1 ||
		req.Plan == nil || req.Reason == "" || req.RequestedBy == "" || req.RecordedAt.IsZero() {
		return CancellationResult{}, invalid("cancellation requires tenant, instance, plan, version, reason, actor and RecordedAt")
	}
	tx, err := d.opts.DB.Begin(ctx)
	if err != nil {
		return CancellationResult{}, fmt.Errorf("workflow execute: begin cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, req.TenantID); err != nil {
		return CancellationResult{}, err
	}
	store := runtime.Store{}
	inst, err := store.LoadInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return CancellationResult{}, err
	}
	if inst.CompiledPlanHash != req.Plan.Digest() {
		return CancellationResult{}, fmt.Errorf("%w: cancellation plan does not match the instance", ErrCancellationAmbiguous)
	}
	if inst.RuntimeStatus.Terminal() {
		return CancellationResult{}, ErrCancellationTerminal
	}
	nodes, err := store.LoadNodeExecutions(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return CancellationResult{}, err
	}
	for _, execution := range nodes {
		node, ok := req.Plan.Node(execution.NodeID)
		if !ok {
			return CancellationResult{}, fmt.Errorf("%w: durable node %s is absent from the pinned plan", ErrCancellationAmbiguous, execution.NodeID)
		}
		if node.EffectClass.IsWrite() && (execution.Status == runtime.NodeSucceeded || execution.Status == runtime.NodeRunning || execution.Status == runtime.NodeWaiting) {
			return CancellationResult{}, fmt.Errorf("%w: effect node %s is %s", ErrCancellationAmbiguous, execution.NodeID, execution.Status)
		}
	}

	if _, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
		TenantID: req.TenantID, InstanceID: req.InstanceID, ExpectedVersion: inst.InstanceVersion,
		Status: runtime.InstanceCancelling, CurrentNodeIDs: inst.CurrentNodeIDs,
		VariableRevisionHead: inst.VariableRevisionHead, EffectiveContextRef: inst.EffectiveContextRef,
		LastCheckpointRef: inst.LastCheckpointRef,
	}); err != nil {
		return CancellationResult{}, err
	}
	cancelled, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
		TenantID: req.TenantID, InstanceID: req.InstanceID, ExpectedVersion: inst.InstanceVersion + 1,
		Status: runtime.InstanceCancelled, CompletionDimensions: runtime.Dimensions{
			RequestState: "CANCELLED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED",
			ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE",
		}, CompletedAt: timePtr(req.RecordedAt),
	})
	if err != nil {
		return CancellationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CancellationResult{}, fmt.Errorf("workflow execute: commit cancellation: %w", err)
	}
	return CancellationResult{Instance: cancelled}, nil
}

func timePtr(t time.Time) *time.Time { t = t.UTC(); return &t }
