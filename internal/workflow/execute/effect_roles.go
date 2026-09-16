package execute

// Authoritative core versus downstream effects on the served path (WF-RUN-037).
//
// The compiler classifies every write-effect node AUTHORITATIVE_CORE,
// DOWNSTREAM_EFFECT or DERIVED_UPDATE and proves a dependent node never gates
// a core. Each node already advances in its own transaction, so the core's
// commit is independent by construction; what this file adds is that a
// dependent node's failure can no longer abort the run that follows it.
// Without [Options.EffectRoles] a StepRunner error on a downstream leg aborted
// the advancement: the instance stayed RUNNING at the leg with nothing to run
// and the committed core outcome was hidden behind an error. With the policy
// configured the driver:
//
//   - turns a dependent node's StepRunner error into a terminal failed outcome
//     (a claimed in-transaction leg runs under a savepoint, so its own partial
//     writes are discarded while the advance transaction survives);
//   - lets the frontier move the instance along the compiled failure route;
//   - records the sealed [runtime.EffectRoleSettlement] -- RECONCILIATION for a
//     downstream effect, REBUILD_FROM_CORE for a derived update, naming the
//     committed core node executions -- in the same advance transaction.
//
// The core's own node execution is never rewritten.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// The error classes a dependent node's absorbed StepRunner error records.
const (
	ErrorClassDownstreamEffectFailed = "DOWNSTREAM_EFFECT_FAILED"
	ErrorClassDerivedUpdateFailed    = "DERIVED_UPDATE_FAILED"
)

// EffectRoleSettlementRecorder writes one sealed settlement through the advance
// transaction. [runtime.EffectRoleSettlementStore] satisfies it.
type EffectRoleSettlementRecorder interface {
	Record(ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, s runtime.EffectRoleSettlement, recordedAt time.Time) (runtime.EffectRoleSettlement, error)
}

// EffectRolePolicy is the served driver's WF-RUN-037 policy.
type EffectRolePolicy struct {
	// Settlements records the settlement. Required.
	Settlements EffectRoleSettlementRecorder
}

func (p *EffectRolePolicy) validate() error {
	if p != nil && p.Settlements == nil {
		return invalid("EffectRoles requires a Settlements recorder")
	}
	return nil
}

// dependentRole reports whether node is a downstream effect or derived update
// whose failure this driver settles instead of aborting on.
func (d *Driver) dependentRole(node workflow.CompiledNode) bool {
	return d.opts.EffectRoles != nil &&
		(node.EffectRole == workflow.RoleDownstreamEffect || node.EffectRole == workflow.RoleDerivedUpdate)
}

// dependentFailure is the terminal failed outcome a dependent node's
// StepRunner error becomes. RetryTerminal takes the failure route at once: an
// error is not an outcome the node's retry budget was declared for.
func dependentFailure(node workflow.CompiledNode) frontier.NodeOutcome {
	class := ErrorClassDownstreamEffectFailed
	if node.EffectRole == workflow.RoleDerivedUpdate {
		class = ErrorClassDerivedUpdateFailed
	}
	return frontier.NodeOutcome{NodeID: node.ID, Failed: true, RetryTerminal: true, ErrorClass: class}
}

// dependentSavepoint is the savepoint a claimed in-transaction dependent leg
// runs under.
const dependentSavepoint = "wf_run_037_dependent_leg"

// runDependentInTx runs a claimed dependent leg under a savepoint. A leg error
// rolls back to the savepoint -- discarding only the leg's own writes -- and
// returns the terminal failed outcome instead of the error, so the advance
// transaction commits the failure route and the settlement.
func runDependentInTx(
	ctx context.Context, ex runtime.Executor, node workflow.CompiledNode,
	run func(context.Context, runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, error),
) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if _, err := ex.Exec(ctx, "SAVEPOINT "+dependentSavepoint); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("workflow execute: open savepoint for %s: %w", node.ID, err)
	}
	outcome, refs, err := run(ctx, ex)
	if err != nil {
		if _, rbErr := ex.Exec(ctx, "ROLLBACK TO SAVEPOINT "+dependentSavepoint); rbErr != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("workflow execute: roll back leg %s: %v; after %w", node.ID, rbErr, err)
		}
		return dependentFailure(node), runtime.GovernanceRefs{}, nil
	}
	if _, err := ex.Exec(ctx, "RELEASE SAVEPOINT "+dependentSavepoint); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("workflow execute: release savepoint for %s: %w", node.ID, err)
	}
	return outcome, refs, nil
}

// settleEffectRole records the settlement of a dependent node whose attempt
// the advancement just settled FAILED, inside tx. Every other advancement --
// a success, a retry, a core, a read, no policy -- records nothing.
func (d *Driver) settleEffectRole(
	ctx context.Context, tx dbport.Tx, run runContext, outcome frontier.NodeOutcome, attempt int, at time.Time,
) error {
	if !outcome.Failed || d.opts.EffectRoles == nil {
		return nil
	}
	node, ok := run.selection.Plan.Node(outcome.NodeID)
	if !ok || !d.dependentRole(node) {
		return nil
	}
	store := runtime.Store{}
	executions, err := store.LoadNodeExecutions(ctx, tx, run.start.TenantID, run.instanceID)
	if err != nil {
		return err
	}
	settled := false
	for _, e := range executions {
		if e.NodeID == node.ID && e.Attempt == attempt && e.Status == runtime.NodeFailed {
			settled = true
		}
	}
	if !settled {
		return nil // a retry: the attempt is not yet the one that fails the leg
	}
	class := outcome.ErrorClass
	if class == "" {
		class = dependentFailure(node).ErrorClass
	}
	settlement, err := runtime.DeriveEffectRoleSettlement(run.selection.Plan, run.instanceID, node.ID, attempt, class, executions)
	if err != nil {
		return fmt.Errorf("workflow execute: settle %s %s: %w", node.EffectRole, node.ID, err)
	}
	if _, err := d.opts.EffectRoles.Settlements.Record(ctx, tx, run.start.TenantID, settlement, at); err != nil {
		return fmt.Errorf("workflow execute: record %s settlement for %s: %w", node.EffectRole, node.ID, err)
	}
	return nil
}
