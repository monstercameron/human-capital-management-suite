package recover

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// StepEffect is the business effect one served node performs, written through
// the transaction it is handed. It is [Effect]'s counterpart for the execute
// driver's own drain: the driver runs a node before its advancement commits,
// so a worker that dies between the two leaves an effect with no advancement,
// and the recovery sweep has to tell "already done" from "never done".
//
// Perform must have no side channel outside tx, for the reason [Effect.Perform]
// gives: a rolled-back transaction cannot un-send a request. A connector
// dispatch is an outbox row written through tx, not a network call.
type StepEffect interface {
	// Guards reports whether node's effect is guarded. A node it does not
	// guard runs through [GuardedSteps.Inner] unchanged.
	Guards(node workflow.CompiledNode) bool
	// Perform dispatches the effect inside tx and reports the node outcome
	// and governance references the advancement runs on.
	Perform(ctx context.Context, tx dbport.Tx, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error)
}

// GuardedSteps is an [execute.StepRunner] that makes a node's business effect
// exactly-once across worker death (WF-RUN-003).
//
// A guarded node's effect runs inside its own tenant transaction under
// TX-006's guard, keyed by the node activation ([StepEffectKey]), and the
// outcome it produced is stored as the record's result identity. When the
// driver dies after that transaction commits and before the advancement does,
// the redelivered drain presents the same key and digest: the guard reports
// the record COMPLETED, the effect is not performed again, and the stored
// outcome is decoded and advanced on ([DispositionReplayResult]). When nothing
// was recorded, the effect runs ([DispositionExecuteEffect]).
//
// The key names the activation attempt, unlike [AttemptKey]. The difference is
// deliberate: [Recoverer] schedules a new attempt for the effect it recovers,
// so its key must survive the attempt number; a redelivered drain re-runs the
// same READY activation, so the attempt number is what distinguishes a lost
// run of this activation (replay) from a legitimate retry or a re-entered node
// (a new effect).
type GuardedSteps struct {
	Inner     execute.StepRunner
	Effects   StepEffect
	DB        Beginner
	Store     idempotency.Store
	Retention idempotency.RetentionPolicy
	// Verifier checks the WORKFLOW_INSTANCE fence the run carries
	// ([execute.FenceFromContext]) inside the effect's transaction, before the
	// guard is reached, so a driver whose lease was taken over by a recovery
	// sweep performs no effect at all. Required: an unfenced guarded effect is
	// the RED case, exactly as for [Recoverer].
	Verifier runtime.FenceVerifier
}

var _ execute.StepRunner = GuardedSteps{}

const (
	stepKeyPrefix    = "wf-step-effect:"
	stepResultPrefix = "wf-step-result/v1:"
)

// StepEffectKey is the idempotency key of one node activation's effect.
func StepEffectKey(instanceID uuid.UUID, nodeID string, attempt int) string {
	return stepKeyPrefix + instanceID.String() + ":" + nodeID + ":" + strconv.Itoa(attempt)
}

// StepEffectRequest is the [Request] whose TX-006 coordinates guard one node
// activation's effect: the node's own effect scope, the plan's capability and
// the activation key.
func StepEffectRequest(req execute.StepRequest) Request {
	return Request{
		TenantID: req.TenantID, InstanceID: req.InstanceID, NodeID: req.Node.ID, Plan: req.Plan,
		IdempotencyKey: StepEffectKey(req.InstanceID, req.Node.ID, req.Attempt),
		CorrelationID:  req.CorrelationID,
	}
}

// Run implements [execute.StepRunner].
func (g GuardedSteps) Run(ctx context.Context, req execute.StepRequest) (ret0 frontier.NodeOutcome, ret1 runtime.GovernanceRefs, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.recover.guarded_step", observe.Attrs{
		observe.KeyTenant: req.TenantID.String(), observe.KeyInstance: req.InstanceID.String(),
		observe.KeyNode: req.Node.ID, observe.KeyAttempt: strconv.Itoa(req.Attempt),
	})
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if g.Effects == nil || !g.Effects.Guards(req.Node) {
		if g.Inner == nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("node %s is not guarded and no inner StepRunner is configured", req.Node.ID)
		}
		return g.Inner.Run(ctx, req)
	}
	if g.DB == nil || g.Store == nil || g.Verifier == nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("a guarded step needs a database Beginner, an idempotency Store and a FenceVerifier")
	}
	fence, fenced := execute.FenceFromContext(ctx)
	if !fenced {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, refuse(CodeFenceRefused, ErrFenceRefused, req.InstanceID.String(), req.Node.ID,
			"a guarded step effect runs only under a WORKFLOW_INSTANCE fence")
	}
	if req.RecordedAt.IsZero() || req.Attempt < 1 {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("a guarded step needs the driver's instant and a positive attempt")
	}
	effect := StepEffectRequest(req)
	if err := effect.validate(); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	r := Recoverer{opts: Options{DB: g.DB}}
	tx, err := r.begin(ctx, req.TenantID)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	instanceID := req.InstanceID.String()
	fence.At = req.RecordedAt.UTC()
	if err := g.Verifier.VerifyFence(ctx, tx, req.TenantID, fence); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, wrap(CodeFenceRefused, ErrFenceRefused, instanceID, req.Node.ID, err,
			"fence token %d presented by %s was refused before the step effect", fence.Token, fence.HolderID)
	}

	var (
		performed bool
		outcome   frontier.NodeOutcome
		refs      runtime.GovernanceRefs
	)
	rec, err := idempotency.Guard(ctx, tx, g.Store, effect.Scope(), effect.EffectDigest(), g.Retention, req.RecordedAt.UTC(),
		func(ctx context.Context, tx dbport.Tx) (idempotency.ResultIdentity, error) {
			o, gr, perr := g.Effects.Perform(ctx, tx, req)
			if perr != nil {
				return idempotency.ResultIdentity{}, perr
			}
			identity, eerr := encodeStepResult(o, gr)
			if eerr != nil {
				return idempotency.ResultIdentity{}, eerr
			}
			performed, outcome, refs = true, o, gr
			return identity, nil
		})
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, wrap(CodeEffectFailed, ErrEffect, instanceID, req.Node.ID, err,
			"dispatch the step effect under idempotency key %q", effect.IdempotencyKey)
	}
	if !performed {
		outcome, refs, err = decodeStepResult(rec.Identity)
		if err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, wrap(CodeEffectFailed, ErrEffect, instanceID, req.Node.ID, err,
				"replay the step result stored under idempotency key %q", effect.IdempotencyKey)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, wrap(CodeStorageFailed, ErrStorage, instanceID, req.Node.ID, err,
			"commit the step effect")
	}
	obsOp.Set(observe.KeyDisposition, string(stepDisposition(performed)))
	return outcome, refs, nil
}

func stepDisposition(performed bool) Disposition {
	if performed {
		return DispositionExecuteEffect
	}
	return DispositionReplayResult
}

// storedStepResult is the result identity payload a guarded step stores: the
// complete outcome and governance references, so a replay advances on exactly
// what the lost run produced.
type storedStepResult struct {
	Outcome frontier.NodeOutcome   `json:"outcome"`
	Refs    runtime.GovernanceRefs `json:"refs"`
}

func encodeStepResult(outcome frontier.NodeOutcome, refs runtime.GovernanceRefs) (idempotency.ResultIdentity, error) {
	b, err := json.Marshal(storedStepResult{Outcome: outcome, Refs: refs})
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("recover: encode step result: %w", err)
	}
	return idempotency.ResultIdentity{ResultRef: stepResultPrefix + string(b)}, nil
}

func decodeStepResult(identity idempotency.ResultIdentity) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	payload, ok := strings.CutPrefix(identity.ResultRef, stepResultPrefix)
	if !ok {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("recover: stored result %q is not a step result", identity.ResultRef)
	}
	var stored storedStepResult
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("recover: decode step result: %w", err)
	}
	return stored.Outcome, stored.Refs, nil
}
