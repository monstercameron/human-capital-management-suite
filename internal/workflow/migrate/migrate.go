package migrate

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrationpreview"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Executor is the minimal database capability this package needs -- the same
// shape [runtime.Executor] and [runtimestate.Executor] each declare, so one
// caller-owned transaction satisfies every store this package writes through.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Request is one caller's request to execute an already-approved migration
// for exactly one paused instance.
type Request struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID

	// SourcePlan is the exact compiled plan the instance is currently pinned
	// to; TargetPlan is the exact compiled plan it migrates onto. Both must
	// digest to exactly what Preview named ([PreviewRecord.SourceDigest] /
	// [PreviewRecord.TargetDigest]).
	SourcePlan *workflow.CompiledWorkflow
	TargetPlan *workflow.CompiledWorkflow

	Preview  PreviewRecord
	Approval Approval

	// MigratedBy and MigratedAt name and stamp the principal and instant
	// executing this call. This package never reads a wall clock.
	MigratedBy string
	MigratedAt time.Time
}

func (r Request) validate() error {
	switch {
	case r.TenantID == uuid.Nil:
		return refuse(CodeInvalidRequest, "", "tenant id must not be the nil UUID")
	case r.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRequest, "", "instance id must not be the nil UUID")
	case r.SourcePlan == nil:
		return refuse(CodeInvalidRequest, r.InstanceID.String(), "no source compiled plan supplied")
	case r.TargetPlan == nil:
		return refuse(CodeInvalidRequest, r.InstanceID.String(), "no target compiled plan supplied")
	case r.MigratedBy == "":
		return refuse(CodeInvalidRequest, r.InstanceID.String(), "migration executor is required")
	case r.MigratedAt.IsZero():
		return refuse(CodeInvalidRequest, r.InstanceID.String(),
			"migrated_at must be supplied; this package never reads a wall clock")
	}
	return r.Approval.validate()
}

// Receipt is the digested evidence one successful [Migrate] call produced.
type Receipt struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID

	FromWorkflowVersion    uint32
	FromCompiledPlanDigest string
	ToWorkflowVersion      uint32
	ToCompiledPlanDigest   string

	FromFrontier []string
	ToFrontier   []string

	// Outcome is the preview classification this migration acted on: SAFE
	// (CONTINUE) or TRANSFORMABLE (BRIDGE).
	Outcome migrationpreview.Outcome
	// BridgeRef names the declared bridge applied, empty for a SAFE/CONTINUE
	// migration that used none.
	BridgeRef string

	PreviewDigest string
	ApprovedBy    string
	ApprovedAt    time.Time
	MigratedBy    string
	MigratedAt    time.Time

	PriorCheckpointSequence uint64
	CheckpointSequence      uint64
	NewInstanceVersion      int64

	digest string
}

// Digest is the receipt's content identity.
func (r Receipt) Digest() string { return r.digest }

type receiptIdentity struct {
	TenantID                string
	InstanceID              string
	FromWorkflowVersion     uint32
	FromCompiledPlanDigest  string
	ToWorkflowVersion       uint32
	ToCompiledPlanDigest    string
	FromFrontier            []string
	ToFrontier              []string
	Outcome                 migrationpreview.Outcome
	BridgeRef               string
	PreviewDigest           string
	ApprovedBy              string
	MigratedBy              string
	PriorCheckpointSequence uint64
	CheckpointSequence      uint64
	NewInstanceVersion      int64
}

func computeReceiptDigest(r Receipt) string {
	return canonicalDigest(receiptDigestProfile, receiptIdentity{
		TenantID: r.TenantID.String(), InstanceID: r.InstanceID.String(),
		FromWorkflowVersion: r.FromWorkflowVersion, FromCompiledPlanDigest: r.FromCompiledPlanDigest,
		ToWorkflowVersion: r.ToWorkflowVersion, ToCompiledPlanDigest: r.ToCompiledPlanDigest,
		FromFrontier: append([]string(nil), r.FromFrontier...), ToFrontier: append([]string(nil), r.ToFrontier...),
		Outcome: r.Outcome, BridgeRef: r.BridgeRef,
		PreviewDigest: r.PreviewDigest, ApprovedBy: r.ApprovedBy, MigratedBy: r.MigratedBy,
		PriorCheckpointSequence: r.PriorCheckpointSequence, CheckpointSequence: r.CheckpointSequence,
		NewInstanceVersion: r.NewInstanceVersion,
	})
}

// activeNodeStatus mirrors internal/workflow/frontier.NodeState.Active() by
// name convention: [runtime.NodeStatus] declares the identical ten-value
// vocabulary but exposes no Active method of its own, and importing frontier
// solely for this one predicate would be a heavier coupling than restating
// the four-value list it already documents.
func activeNodeStatus(s runtime.NodeStatus) bool {
	switch s {
	case runtime.NodeReady, runtime.NodeRunning, runtime.NodeWaiting, runtime.NodeRetrying:
		return true
	default:
		return false
	}
}

func latestExecution(rows []runtime.NodeExecution, nodeID string) (runtime.NodeExecution, bool) {
	var latest runtime.NodeExecution
	found := false
	for _, r := range rows {
		if r.NodeID != nodeID {
			continue
		}
		if !found || r.Attempt > latest.Attempt {
			latest = r
			found = true
		}
	}
	return latest, found
}

// Migrate executes one approved migration for a single paused instance inside
// tx, which the caller began and will commit or roll back.
//
// Every precondition WF-RUN-018 names is checked before any statement writes:
// the instance must be PAUSED at the exact checkpoint the preview classified,
// the preview's classification for this instance must be CONTINUE (SAFE) or
// BRIDGE (TRANSFORMABLE) -- STRANDED (IMPOSSIBLE) and REQUIRES_REPAIR both
// refuse -- the presented plans' digests must still be the ones the preview
// was run against, and the approval must name a distinct approver reviewing
// exactly that preview's own digest. A refusal at any of those gates, or a
// failure partway through applying the transform, leaves every statement this
// call issued to be rolled back by the caller: nothing here commits its own
// transaction, and a failed call never rewrites the instance's pinned version.
//
// On success the instance's workflow version, compiled-plan digest and
// frontier move together in one write ([runtime.Store.RecordVersionMigration])
// alongside whatever node-execution transform the classification requires,
// and a digested MIGRATION checkpoint is left behind as the migration's own
// evidence. The instance is left PAUSED on the new version: a caller resumes
// it separately through [runtime.ResumeFromPause] with the target plan once
// it is satisfied the migration committed.
func Migrate(ctx context.Context, tx Executor, req Request) (ret0 Receipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.migrate", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return Receipt{}, err
	}
	if req.Approval.PreviewDigest != req.Preview.Digest() {
		return Receipt{}, refuse(CodeUnapprovedDigest, req.InstanceID.String(),
			"approval names preview digest %s; the presented preview record digests to %s",
			req.Approval.PreviewDigest, req.Preview.Digest())
	}
	if req.Approval.Approver == req.MigratedBy {
		return Receipt{}, refuse(CodeSeparationOfDuties, req.InstanceID.String(),
			"migration approver must be distinct from the principal executing the migration")
	}
	if req.SourcePlan.Digest() != req.Preview.SourceDigest || req.TargetPlan.Digest() != req.Preview.TargetDigest {
		return Receipt{}, refuse(CodeStalePreview, req.InstanceID.String(),
			"source or target compiled-plan digest has moved since the preview was taken; re-run the preview")
	}
	if req.SourcePlan.WorkflowID != req.TargetPlan.WorkflowID {
		return Receipt{}, refuse(CodePlanMismatch, req.InstanceID.String(),
			"source and target plans name different workflows")
	}

	store := runtime.Store{}
	inst, err := store.LoadInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return Receipt{}, err
	}
	if inst.CompiledPlanHash != req.SourcePlan.Digest() {
		return Receipt{}, refuse(CodePlanMismatch, req.InstanceID.String(),
			"instance pins compiled plan %s; migration source plan digests to %s",
			inst.CompiledPlanHash, req.SourcePlan.Digest())
	}
	if inst.RuntimeStatus != runtime.InstancePaused {
		return Receipt{}, refuse(CodeUnsafePoint, req.InstanceID.String(),
			"instance is %s, not PAUSED; a migration requires a reviewed safe point", string(inst.RuntimeStatus))
	}
	eligible, blocking, err := runtime.SafePointEligibility(ctx, tx, inst, req.SourcePlan)
	if err != nil {
		return Receipt{}, err
	}
	if !eligible {
		return Receipt{}, refuse(CodeUnsafePoint, req.InstanceID.String(),
			"frontier node %s is not an eligible safe point", blocking)
	}
	if len(inst.CurrentNodeIDs) != 1 {
		return Receipt{}, refuse(CodeMultiNodeFrontier, req.InstanceID.String(),
			"instance carries a %d-node frontier; this package migrates only a single-node frontier",
			len(inst.CurrentNodeIDs))
	}
	currentNodeID := inst.CurrentNodeIDs[0]

	checkpoints := runtimestate.CheckpointStore{}
	checkpoint, err := checkpoints.Latest(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return Receipt{}, wrap(CodeUnsafePoint, req.InstanceID.String(), err,
			"no durable checkpoint describes this instance")
	}
	if checkpoint.InstanceVersion != uint64(inst.InstanceVersion) {
		return Receipt{}, refuse(CodeUnsafePoint, req.InstanceID.String(),
			"latest checkpoint describes instance version %d; the instance now reads %d",
			checkpoint.InstanceVersion, inst.InstanceVersion)
	}
	if checkpoint.FrontierDigest != FrontierDigest(inst.CurrentNodeIDs) {
		return Receipt{}, refuse(CodeUnsafePoint, req.InstanceID.String(),
			"latest checkpoint's frontier digest does not describe the instance's current frontier")
	}

	rows, err := store.LoadNodeExecutions(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return Receipt{}, err
	}
	currentAttempt, ok := latestExecution(rows, currentNodeID)
	if !ok {
		return Receipt{}, refuse(CodeUnsafePoint, req.InstanceID.String(),
			"no recorded execution for frontier node %s", currentNodeID)
	}

	assessment, found := req.Preview.Assessment(req.InstanceID.String())
	if !found {
		return Receipt{}, refuse(CodeNotPreviewed, req.InstanceID.String(), "the preview never classified this instance")
	}
	if assessment.Source.StepID != currentNodeID || assessment.Source.Stage != string(currentAttempt.Status) {
		return Receipt{}, refuse(CodeChangedInstance, req.InstanceID.String(),
			"instance now sits at %s/%s; the preview classified it at %s/%s",
			currentNodeID, string(currentAttempt.Status), assessment.Source.StepID, assessment.Source.Stage)
	}

	switch assessment.Outcome {
	case migrationpreview.OutcomeSafe, migrationpreview.OutcomeTransformable:
		// proceed
	case migrationpreview.OutcomeImpossible:
		return Receipt{}, refuse(CodeStranded, req.InstanceID.String(),
			"preview classified this instance STRANDED (IMPOSSIBLE); no legal target exists")
	default:
		return Receipt{}, refuse(CodeRequiresRepair, req.InstanceID.String(),
			"preview classified this instance %s; only SAFE or TRANSFORMABLE may migrate", assessment.Outcome)
	}
	if assessment.Target == nil {
		return Receipt{}, refuse(CodeRequiresRepair, req.InstanceID.String(), "preview named no target state for this instance")
	}

	targetStatus := runtime.NodeStatus(assessment.Target.Stage)
	if !targetStatus.Valid() || !activeNodeStatus(targetStatus) {
		return Receipt{}, refuse(CodeInvalidBridgeStage, req.InstanceID.String(),
			"target stage %q is not a declared, active runtime node status", assessment.Target.Stage)
	}
	if _, ok := req.TargetPlan.Node(assessment.Target.StepID); !ok {
		return Receipt{}, refuse(CodePlanMismatch, req.InstanceID.String(),
			"preview names target step %q, which the target plan does not declare", assessment.Target.StepID)
	}

	version := inst.InstanceVersion
	newFrontier := []string{currentNodeID}
	bridgeRef := ""
	if assessment.Bridge != nil {
		bridgeRef = assessment.Bridge.Ref
	}

	if assessment.Target.StepID == currentNodeID {
		if targetStatus != currentAttempt.Status {
			ne := runtime.NewNodeExecution(req.TenantID, req.InstanceID, currentNodeID,
				currentAttempt.Attempt+1, currentAttempt.StepType, targetStatus)
			_, version, err = store.RecordNodeExecution(ctx, tx, ne, version)
			if err != nil {
				return Receipt{}, err
			}
		}
	} else {
		if currentAttempt.Status == runtime.NodeRetrying {
			return Receipt{}, refuse(CodeUnsupportedBridgeSource, req.InstanceID.String(),
				"a bridge away from a RETRYING attempt is refused; retry to READY or WAITING first")
		}
		migratedAt := req.MigratedAt
		_, version, err = store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{
			TenantID: req.TenantID, InstanceID: req.InstanceID, NodeID: currentNodeID, Attempt: currentAttempt.Attempt,
			ExpectedInstanceVersion: version,
			Status:                  runtime.NodeCancelled,
			ErrorClass:              "MIGRATED",
			TraceID:                 bridgeRef,
			CompletedAt:             &migratedAt,
		})
		if err != nil {
			return Receipt{}, err
		}
		targetNode, _ := req.TargetPlan.Node(assessment.Target.StepID)
		ne := runtime.NewNodeExecution(req.TenantID, req.InstanceID, assessment.Target.StepID, 1, targetNode.Type, targetStatus)
		_, version, err = store.RecordNodeExecution(ctx, tx, ne, version)
		if err != nil {
			return Receipt{}, err
		}
		newFrontier = []string{assessment.Target.StepID}
	}

	updated, err := store.RecordVersionMigration(ctx, tx, runtime.VersionMigration{
		TenantID: req.TenantID, InstanceID: req.InstanceID, ExpectedVersion: version,
		NewWorkflowVersion: req.TargetPlan.Version, NewCompiledPlanHash: req.TargetPlan.Digest(),
		NewCurrentNodeIDs: newFrontier,
	})
	if err != nil {
		return Receipt{}, err
	}

	receipt := Receipt{
		TenantID: req.TenantID, InstanceID: req.InstanceID,
		FromWorkflowVersion: req.SourcePlan.Version, FromCompiledPlanDigest: req.SourcePlan.Digest(),
		ToWorkflowVersion: req.TargetPlan.Version, ToCompiledPlanDigest: req.TargetPlan.Digest(),
		FromFrontier: []string{currentNodeID}, ToFrontier: append([]string(nil), newFrontier...),
		Outcome: assessment.Outcome, BridgeRef: bridgeRef,
		PreviewDigest: req.Preview.Digest(), ApprovedBy: req.Approval.Approver, ApprovedAt: req.Approval.ApprovedAt.UTC(),
		MigratedBy: req.MigratedBy, MigratedAt: req.MigratedAt.UTC(),
		PriorCheckpointSequence: checkpoint.Sequence, CheckpointSequence: checkpoint.Sequence + 1,
		NewInstanceVersion: updated.InstanceVersion,
	}
	receipt.digest = computeReceiptDigest(receipt)

	if err := checkpoints.Take(ctx, tx, runtimestate.Checkpoint{
		TenantID: req.TenantID, InstanceID: req.InstanceID, Sequence: receipt.CheckpointSequence,
		Kind:            runtimestate.CheckpointMigration,
		StateDigest:     receipt.Digest(),
		FrontierDigest:  FrontierDigest(newFrontier),
		VariableDigest:  checkpoint.VariableDigest,
		InstanceVersion: uint64(updated.InstanceVersion),
		TakenAt:         req.MigratedAt,
	}); err != nil {
		return Receipt{}, wrap(CodeInvalidRequest, req.InstanceID.String(), err, "record migration checkpoint")
	}

	return receipt, nil
}
