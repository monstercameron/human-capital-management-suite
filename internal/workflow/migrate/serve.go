package migrate

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrationpreview"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// PreviewPausedInstance classifies one paused instance against a target plan
// by reading its live frontier from the same stores Migrate later writes
// through, then sealing the result as a PreviewRecord. It is the operator
// entry point REV-009-01 wires into the served cell: a caller supplies the
// exact source and target plans (resolved from the version registry) and any
// declared bridges, and this function derives the LiveInstanceState from
// durable state instead of trusting a caller-supplied step/stage pair.
//
// The instance must be PAUSED on a single-node frontier with a recorded node
// execution, and the pinned plan hash must still equal the source plan's
// digest; anything else is refused before Preview runs. A preview that
// classifies the instance STRANDED or REQUIRES_REPAIR is returned as an
// error alongside the sealed record, so an operator keeps the evidence while
// the migration stays refused.
func PreviewPausedInstance(ctx context.Context, tx Executor, tenantID, instanceID uuid.UUID, source, target *workflow.CompiledWorkflow, bridges []migrationpreview.Bridge) (ret0 PreviewRecord, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.preview_paused_instance", tenantID.String(), instanceID.String())
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if tenantID == uuid.Nil || instanceID == uuid.Nil {
		return PreviewRecord{}, refuse(CodeInvalidRequest, instanceID.String(), "tenant and instance ids must not be the nil UUID")
	}
	if source == nil || target == nil {
		return PreviewRecord{}, refuse(CodeInvalidRequest, instanceID.String(), "source and target compiled plans are required")
	}
	store := runtime.Store{}
	inst, err := store.LoadInstance(ctx, tx, tenantID, instanceID)
	if err != nil {
		return PreviewRecord{}, err
	}
	if inst.CompiledPlanHash != source.Digest() {
		return PreviewRecord{}, refuse(CodePlanMismatch, instanceID.String(),
			"instance pins compiled plan %s; preview source plan digests to %s",
			inst.CompiledPlanHash, source.Digest())
	}
	if inst.RuntimeStatus != runtime.InstancePaused {
		return PreviewRecord{}, refuse(CodeUnsafePoint, instanceID.String(),
			"instance is %s, not PAUSED; a migration preview requires a reviewed safe point", string(inst.RuntimeStatus))
	}
	if len(inst.CurrentNodeIDs) != 1 {
		return PreviewRecord{}, refuse(CodeMultiNodeFrontier, instanceID.String(),
			"instance carries a %d-node frontier; this package previews only a single-node frontier",
			len(inst.CurrentNodeIDs))
	}
	currentNodeID := inst.CurrentNodeIDs[0]
	rows, err := store.LoadNodeExecutions(ctx, tx, tenantID, instanceID)
	if err != nil {
		return PreviewRecord{}, err
	}
	currentAttempt, ok := latestExecution(rows, currentNodeID)
	if !ok {
		return PreviewRecord{}, refuse(CodeUnsafePoint, instanceID.String(),
			"no recorded execution for frontier node %s", currentNodeID)
	}
	live := migrationpreview.LiveInstanceState{
		InstanceID: instanceID.String(),
		StepID:     currentNodeID,
		Stage:      string(currentAttempt.Status),
	}
	result, previewErr := migrationpreview.Preview(migrationpreview.Request{
		Source: source, Target: target,
		Instances: []migrationpreview.LiveInstanceState{live},
		Bridges:   bridges,
	})
	rec, capErr := CapturePreview(source, target, result)
	if capErr != nil {
		return PreviewRecord{}, capErr
	}
	if previewErr != nil {
		return rec, previewErr
	}
	return rec, nil
}
