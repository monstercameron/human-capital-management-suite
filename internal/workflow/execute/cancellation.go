package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var (
	// ErrCancellationAmbiguous means an irreversible effect has run without a
	// durable observation that proves it is safe to cancel: the governed
	// decision is CANNOT_CANCEL, recorded, and the instance is untouched.
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

// CancellationResult reports the governed decision and the instance it left.
// Instance is CANCELLED only when Decision is CANCELLED; a
// COMPENSATION_REQUIRED instance is CANCELLING and a REPAIR_REQUIRED one is
// REPAIR_REQUIRED.
type CancellationResult struct {
	Instance   runtime.Instance
	Decision   workflow.CancellationDecision
	DecisionID uuid.UUID
	Evidence   workflow.CancellationOutcome
}

// Cancel runs the governed cancellation (internal/workflow/cancellation) over
// the instance's durable facts in one transaction. A CANNOT_CANCEL decision
// is recorded and returned together with [ErrCancellationAmbiguous]; the
// instance is never claimed cancelled while an effect or child is unresolved.
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
	inst, err := (runtime.Store{}).LoadInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return CancellationResult{}, err
	}
	if inst.CompiledPlanHash != req.Plan.Digest() {
		return CancellationResult{}, fmt.Errorf("%w: cancellation plan does not match the instance", ErrCancellationAmbiguous)
	}
	if inst.RuntimeStatus.Terminal() {
		return CancellationResult{}, ErrCancellationTerminal
	}
	out, err := cancellation.Decide(ctx, tx, cancellation.Request{
		TenantID: req.TenantID, InstanceID: req.InstanceID, ExpectedInstanceVersion: req.ExpectedInstanceVersion,
		Plan: req.Plan, Reason: req.Reason, RequestedBy: req.RequestedBy, RecordedAt: req.RecordedAt,
	})
	if errors.Is(err, cancellation.ErrTerminal) {
		return CancellationResult{}, fmt.Errorf("%w: %w", ErrCancellationTerminal, err)
	}
	if err != nil {
		return CancellationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CancellationResult{}, fmt.Errorf("workflow execute: commit cancellation: %w", err)
	}
	result := CancellationResult{Instance: out.Instance, Decision: out.Decision, DecisionID: out.DecisionID, Evidence: out.Evidence}
	if out.Decision == workflow.CannotCancel {
		reason, _ := out.Blocking()
		return result, fmt.Errorf("%w: %s %s", ErrCancellationAmbiguous, reason.Code, reason.Ref)
	}
	return result, nil
}
