package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

// The write half of the journey engine: the two governed actions the page
// offers. Execute is [IntentService.ExecuteIntent] behind the P1B
// execution-authority gate; Decide is the real WorkItem claim/start/complete
// followed by the caller-driven driver's Resume. Neither invents a path: what
// the page does is what an operator and an approver would do through the API.

// journeyClaimWindow is how long the approval claim the engine takes on the
// routed approver's behalf is held. The whole decision is made and committed
// inside one transaction, so the window only has to be positive; an hour is
// the same order the driver's own 48-hour work-item deadline uses.
const journeyClaimWindow = time.Hour

// Reasons the journey records on the WorkItem transitions it writes. They are
// this engine's own vocabulary and are read back on the journey timeline.
const (
	journeyReasonClaimed = "journey.approval.claimed"
	journeyReasonStarted = "journey.approval.started"
	journeyReasonDecided = "journey.approval.decided"
)

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

// Execute implements [workspace.JourneyEngine].
//
// It is exactly the wire flow an operator drives: GetIntent, SimulateIntent,
// then ExecuteIntent with the approval naming the revision and digest the
// re-simulation just minted. The authority gate, the re-simulation and the
// approval check all happen inside ExecuteIntent; this method adds only the
// one thing the page needs and the RPC does not have: a stage guard, so
// pressing Execute twice reports a refusal rather than silently replaying the
// driver's idempotent start.
func (e *journeyEngine) Execute(ctx context.Context, intentID string) (workspace.JourneyDetail, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return workspace.JourneyDetail{}, err
	}
	got, getErr := e.svc.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: intentID})
	if getErr != nil {
		return workspace.JourneyDetail{}, journeyError(getErr)
	}
	if got.GetIntent().GetDefinition().GetIntentTypeId() != promotion.IntentType {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: %s is not a promotion journey",
			workspace.ErrJourneyUnknown, intentID)
	}
	// PROMOUX-013: the same terminal-RequestState guard [journeyEngine.Decide]
	// applies, stated over the wire enum since this method reads the proto
	// form rather than the kernel Instance.
	switch got.GetIntent().GetLifecycle().GetRequest() {
	case intentsv1.RequestState_REQUEST_STATE_CANCELLED, intentsv1.RequestState_REQUEST_STATE_SUPERSEDED,
		intentsv1.RequestState_REQUEST_STATE_REJECTED, intentsv1.RequestState_REQUEST_STATE_WITHDRAWN,
		intentsv1.RequestState_REQUEST_STATE_CLOSED:
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this journey's proposal has been %s and can no longer be executed",
			workspace.ErrJourneyStage, strings.ToLower(got.GetIntent().GetLifecycle().GetRequest().String()))
	}
	simulated, simErr := e.resimulateDetailed(ctx, intentID)
	if simErr != nil {
		return workspace.JourneyDetail{}, simErr
	}
	artifact := simulated.Artifact
	if simulated.Revision == nil {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: executable simulation returned no minted proposal revision", workspace.ErrJourneyStage)
	}

	if _, running, guardErr := e.instanceOf(ctx, principal, artifact); guardErr != nil {
		return workspace.JourneyDetail{}, guardErr
	} else if running {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this promotion has already been executed", workspace.ErrJourneyStage)
	}

	// WF-RUN-027: runtime.Start no longer takes this page's word that the
	// proposal may run -- it reads the decisions recorded against the exact
	// revision. The execution-authority gate's own admission is that decision,
	// so it is committed here, bound to the revision's material digest and the
	// control context it was seen under, before the driver is asked to start.
	// The gate is evaluated first (ExecuteIntent evaluates it again, and
	// records the evidence): a caller the authority refuses must not leave an
	// AUTHZ decision behind saying it was admitted.
	if admitErr := e.admitExecution(ctx, principal, intentID, artifact, *simulated.Revision); admitErr != nil {
		return workspace.JourneyDetail{}, admitErr
	}

	if _, execErr := e.svc.ExecuteIntent(ctx, &intentsv1.ExecuteIntentRequest{
		IdempotencyKey:          "journey:execute:" + intentID,
		IntentId:                intentID,
		ExpectedInstanceVersion: got.GetIntent().GetInstanceVersion(),
		Approval: &intentsv1.ProposalApproval{
			ProposalRevisionId:     artifact.GetProposalRevisionId(),
			MaterialProposalDigest: artifact.GetMaterialProposalDigest(),
			Approved:               true,
			ApprovalRef:            journeyApprovalRef(intentID),
		},
	}); execErr != nil {
		return workspace.JourneyDetail{}, journeyError(execErr)
	}
	return e.Inspect(ctx, intentID)
}

// admitExecution runs the execution-authority gate and, when this cell reads
// WF-RUN-027's durable facts, commits the AUTHZ decision that admits this
// exact proposal revision to execution.
//
// The decision is the durable replacement for the caller-asserted
// runtime.ProposalBinding.Approved this page used to present: it names the
// requirement (the execution-authority rule), the principal who exercised it,
// the authority reference the gate stands on, the revision's own material
// digest and the control snapshots the intent pinned. runtime.Start reads it
// back through [DurableProposalFacts] inside its own start transaction, so a
// caller cannot invent one, and one recorded against a different digest is
// refused as an approval-binding mismatch rather than accepted.
//
// A cell composed with no execution facts cannot start: it has no durable
// source from which runtime.Start can derive approval and supersession facts.
func (e *journeyEngine) admitExecution(
	ctx context.Context, principal *trust.Principal, intentID string, artifact *intentsv1.SimulationArtifact, revision intent.ProposalRevision,
) error {
	inst, _, ownedErr := e.svc.loadInstance(ctx, principal.Tenant().String(), intentID)
	if ownedErr != nil {
		return journeyError(ownedErr)
	}
	def, defErr := e.svc.defs.Resolve(inst.Definition)
	if defErr != nil {
		return fmt.Errorf("%w: %s", workspace.ErrJourneyUnknown, intentID)
	}
	if gateErr := e.svc.authorizeExecution(principal, def); gateErr != nil {
		return journeyError(gateErr)
	}
	if e.svc.executionFacts == nil {
		return nil
	}
	intentUUID, parseErr := executionIntentUUID(inst.IntentID)
	if parseErr != nil {
		return parseErr
	}
	decision := executionDecision{
		TenantID:         e.svc.tenantUUID(principal.Tenant()),
		IntentID:         intentUUID,
		Revision:         simulationRevision,
		MaterialDigest:   artifact.GetMaterialProposalDigest().GetDigest(),
		ControlDigest:    controlSnapshotDigest(inst.ControlSnapshots),
		RequirementID:    executionAuthorityRequirementID,
		Kind:             intentcontrol.DecisionAuthZ,
		Outcome:          intentcontrol.OutcomeApproved,
		DecidedBy:        principal.Subject(),
		AuthorityRef:     e.svc.executionAuthority.decisionRef(),
		Reason:           "the execution authority admits this caller to run this proposal revision",
		DecidedAt:        e.now().UTC(),
		Proposal:         &revision,
		ProposalVerifier: e.svc.digester,
		TenantUUID:       e.svc.tenantUUID,
	}
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := decision.record(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("app: journey: commit the execution authorization: %w", err)
	}
	return nil
}

// journeyApprovalRef is the recorded approval reference the journey presents
// to ExecuteIntent and rebuilds for the resume's own Start. It is derived
// from the intent id so the two calls cannot disagree.
func journeyApprovalRef(intentID string) string { return "approval:journey:" + intentID }

// instanceOf reports whether this journey already has a workflow instance,
// and which one. It is a read of the same durable link [locateInstance]
// documents.
func (e *journeyEngine) instanceOf(
	ctx context.Context, principal *trust.Principal, artifact *intentsv1.SimulationArtifact,
) (runtime.Instance, bool, error) {
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return runtime.Instance{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return e.instanceIn(ctx, tx, principal, artifact.GetMaterialProposalDigest().GetDigest())
}

// instanceIn is [instanceOf] against a transaction the caller already holds.
func (e *journeyEngine) instanceIn(
	ctx context.Context, tx dbport.Tx, principal *trust.Principal, materialDigest string,
) (runtime.Instance, bool, error) {
	tenantID := e.svc.tenantUUID(principal.Tenant())
	instanceID, found, err := e.locateInstance(ctx, tx, tenantID, materialDigest)
	if err != nil || !found {
		return runtime.Instance{}, false, err
	}
	instance, err := (runtime.Store{}).LoadInstance(ctx, tx, tenantID, instanceID)
	if err != nil {
		return runtime.Instance{}, false, fmt.Errorf("app: journey: load the workflow instance: %w", err)
	}
	return instance, true, nil
}

// ---------------------------------------------------------------------------
// Decide
// ---------------------------------------------------------------------------

// Decide implements [workspace.JourneyEngine].
//
// It performs the approver's half of the journey against the real durable
// record, in two steps that are exactly the seam
// internal/workflow/execute.Driver leaves its caller:
//
//  1. In one tenant-scoped transaction, claim, start and complete the routed
//     approval WorkItem through internal/humanwork/workitem's own store and
//     internal/workflow/steps/approval.Complete, with an immutable
//     [intentapproval.ApprovalDecision] built only from durable facts (the
//     routed assignment's own requirement identity, the re-simulated
//     proposal, the configured approver, one instant); then resolve the
//     completed item through internal/workflow/steps/approval.Resolve against
//     the continuation rebuilt from the same durable facts, and commit.
//  2. Resume the driver from the completed item with the resolved outcome,
//     which routes the APPROVED or REJECTED edge, reaches a terminal and
//     records the one governed ledger fact the END node raises.
//
// PROMOUX-015: the decision is made and recorded as the caller. The caller
// must be a member the open WorkItem admits (its ASSIGNEE, or an eligible
// CANDIDATE), may not be the journey's initiator, and may not have decided a
// sibling approval of the same proposal; the claim, the completion, the
// work_item_decision body and the intent_decision row all name the caller.
// The execution role gates EXECUTE only: holding it never makes a non-member
// an approver, and lacking it never stops the routed approver.
func (e *journeyEngine) Decide(ctx context.Context, intentID string, d workspace.Decision) (workspace.JourneyDetail, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return workspace.JourneyDetail{}, err
	}
	inst, rec, ownedErr := e.svc.loadInstance(ctx, principal.Tenant().String(), intentID)
	if ownedErr != nil {
		return workspace.JourneyDetail{}, journeyError(ownedErr)
	}
	def, defErr := e.svc.defs.Resolve(inst.Definition)
	if defErr != nil {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: %s", workspace.ErrJourneyUnknown, intentID)
	}
	if def.Ref.TypeID != promotion.IntentType {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: %s is not a promotion journey",
			workspace.ErrJourneyUnknown, intentID)
	}
	// PROMOUX-013: a decision against an intent CancelIntent has already
	// moved to a terminal RequestState must never silently complete the
	// approval WorkItem and resume the driver -- that is exactly the
	// material-approval invalidation this todo's GREEN clause names.
	// CancelIntent's own kernel transition stops here, at the intent's own
	// dimension tuple; nothing upstream of this method otherwise consults
	// it before claiming and completing a WorkItem, discovered by running
	// TestTodo_PROMOUX_013_Integration/EditInvalidatesAMidFlightApproval
	// against real PostgreSQL.
	switch inst.Lifecycle.Request {
	case lifecycle.RequestCancelled, lifecycle.RequestSuperseded, lifecycle.RequestRejected,
		lifecycle.RequestWithdrawn, lifecycle.RequestClosed:
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this journey's proposal has been %s and can no longer be decided",
			workspace.ErrJourneyStage, strings.ToLower(string(inst.Lifecycle.Request)))
	}
	if gateErr := e.svc.authorizeDecision(def); gateErr != nil {
		return workspace.JourneyDetail{}, journeyError(gateErr)
	}
	if e.svc.executor == nil {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this cell was composed with no execution driver", workspace.ErrJourneyUnavailable)
	}

	simulated, simErr := e.resimulateDetailed(ctx, intentID)
	if simErr != nil {
		return workspace.JourneyDetail{}, simErr
	}
	artifact := simulated.Artifact
	if artifact.GetProposalRevisionId() == "" {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this promotion has no executable proposal", workspace.ErrJourneyStage)
	}
	if simulated.Revision == nil {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: executable simulation returned no minted proposal revision", workspace.ErrJourneyStage)
	}
	start, startErr := e.svc.executionStart(inst, artifact, journeyApprovalRef(intentID), *simulated.Revision)
	if startErr != nil {
		return workspace.JourneyDetail{}, journeyError(startErr)
	}

	done, decideErr := e.completeApproval(ctx, principal, inst, start.Proposal.Revision, d, e.now().UTC(), "", "")
	if decideErr != nil {
		return workspace.JourneyDetail{}, journeyDecisionError(decideErr)
	}
	if !done.replayed {
		result, resumeErr := e.svc.executor.Resume(ctx, ExecutionResumeRequest{
			Start:                   start,
			InstanceID:              done.instance.InstanceID,
			ExpectedInstanceVersion: done.instance.InstanceVersion,
			WorkItem:                done.item,
			Outcome:                 done.outcome,
		})
		if resumeErr != nil {
			return workspace.JourneyDetail{}, journeyError(executionError(resumeErr))
		}
		if outcomeErr := e.svc.consumeExecutionResult(ctx, inst, def, rec, result); outcomeErr != nil {
			return workspace.JourneyDetail{}, outcomeErr
		}
	}
	return e.Inspect(ctx, intentID)
}

// decidedApproval is what one committed decision leaves behind: the
// completed WorkItem row, the instance it belongs to, the exact decision that
// was recorded on it, and the typed step outcome the resume presents.
type decidedApproval struct {
	item      workitem.WorkItem
	instance  runtime.Instance
	decision  intentapproval.ApprovalDecision
	outcome   frontier.NodeOutcome
	replayed  bool
	execution ExecutionResult
}

// completeProposalDecision is the one durable implementation shared by the
// public IntentService operations and the journey page. The service has
// already re-simulated and authority-gated the request; this method binds the
// server-held WorkItem assignment, applies separation of duties and commits
// the decision with completion before the service resumes the driver.
func (e *journeyEngine) completeProposalDecision(
	ctx context.Context,
	principal *trust.Principal,
	inst intent.Instance,
	start runtime.StartRequest,
	req ProposalDecisionRequest,
	approve bool,
) (decidedApproval, error) {
	// PROMOUX-015: membership, the initiator and the sibling approvals are
	// checked by completeApproval against the open WorkItem itself, so this
	// entry point and JourneyEngine.Decide apply one rule.
	if req.ProposalRevisionID != start.Proposal.Revision.ProposalRevisionID ||
		req.MaterialProposalDigest != start.Proposal.Revision.MaterialDigest.Digest {
		return decidedApproval{}, ErrProposalDecisionStale
	}
	decision := workspace.Decision{Approve: approve, Reason: req.Reason}
	return e.completeApproval(ctx, principal, inst, start.Proposal.Revision, decision, e.now().UTC(), req.RequirementID, req.RenderedProjectionDigest)
}

// completeApproval claims, starts and completes the journey's routed approval
// WorkItem in one tenant-scoped transaction, resolves the completed item
// through internal/workflow/steps/approval.Resolve inside that same
// transaction, and commits only when the resolution produced a routable
// outcome. A binding the resolver refuses rolls the completion back, so the
// durable record never holds a decided item the driver could not resume from.
func (e *journeyEngine) completeApproval(
	ctx context.Context,
	principal *trust.Principal,
	inst intent.Instance,
	revision intent.ProposalRevision,
	d workspace.Decision,
	decidedAt time.Time,
	expectedRequirementID string,
	expectedProjectionDigest string,
) (decidedApproval, error) {
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return decidedApproval{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	instance, found, err := e.instanceIn(ctx, tx, principal, revision.MaterialDigest.Digest)
	if err != nil {
		return decidedApproval{}, err
	}
	if !found {
		return decidedApproval{}, fmt.Errorf(
			"%w: this promotion has not been executed yet", workspace.ErrJourneyStage)
	}
	tenantID := e.svc.tenantUUID(principal.Tenant())
	store := workitem.Store{}
	items, err := store.ListForInstance(ctx, tx, tenantID, instance.InstanceID)
	if err != nil {
		return decidedApproval{}, fmt.Errorf("app: journey: list the work items: %w", err)
	}
	item, open := openJourneyWorkItem(items)
	if !open {
		for _, completed := range items {
			if completed.Kind != workitem.KindApproval || completed.Status != workitem.StatusCompleted ||
				completed.ProposalRef != revision.MaterialDigest.Digest || completed.CompletedBy != principal.Subject() ||
				(expectedRequirementID != "" && completed.ApprovalRequirementRef != expectedRequirementID) {
				continue
			}
			record, loadErr := workitem.LoadDecision(ctx, tx, tenantID, completed.WorkItemID)
			if loadErr != nil {
				return decidedApproval{}, fmt.Errorf("%w: load the existing approval decision: %v", ErrProposalDecisionConflict, loadErr)
			}
			existing, decodeErr := proposalDecisionFromRecord(record)
			if decodeErr != nil {
				return decidedApproval{}, decodeErr
			}
			if existing.Outcome != intentapproval.OutcomeApproved && d.Approve ||
				existing.Outcome == intentapproval.OutcomeApproved && !d.Approve {
				return decidedApproval{}, ErrProposalDecisionConflict
			}
			if expectedProjectionDigest != "" && existing.Binding.RenderedProjectionDigest != expectedProjectionDigest {
				return decidedApproval{}, ErrProposalDecisionStale
			}
			outcome, outcomeErr := journeyApprovalOutcome(completed, revision, existing, existing.DecidedAt.Time())
			if outcomeErr != nil {
				return decidedApproval{}, outcomeErr
			}
			return decidedApproval{item: completed, instance: instance, decision: existing, outcome: outcome, replayed: true}, nil
		}
		return decidedApproval{}, fmt.Errorf("%w: this promotion has no open approval to decide", ErrProposalDecisionStage)
	}
	if expectedRequirementID != "" && item.ApprovalRequirementRef != expectedRequirementID {
		return decidedApproval{}, ErrProposalDecisionStale
	}
	if expectedProjectionDigest != "" && item.AssignmentDigest != expectedProjectionDigest {
		return decidedApproval{}, ErrProposalDecisionStale
	}
	// A decision after the routed deadline would complete the item and then
	// resolve to EXPIRED, cancelling the run on the approver's own click.
	// Refusing before the claim leaves the item open for the deadline
	// handling a later phase owns, and tells the approver why.
	if !decidedAt.Before(item.DeadlineAt) {
		return decidedApproval{}, fmt.Errorf("%w: the approval deadline %s has passed", ErrProposalDecisionExpired, item.DeadlineAt.UTC().Format(time.RFC3339))
	}

	// PROMOUX-015: the decider is the caller, and only a caller the open item
	// admits may decide it. Every refusal below happens before the claim, so a
	// refused caller leaves no transition behind.
	actor := principal.Subject()
	candidate, err := journeyDecider(items, item, inst, actor, decidedAt)
	if err != nil {
		return decidedApproval{}, err
	}
	claimed, err := store.ClaimCurrent(ctx, tx, workitem.ClaimCurrentInput{
		TenantID: tenantID, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
		ClaimantPrincipalID: actor,
		ClaimExpiresAt:      decidedAt.Add(journeyClaimWindow), Now: decidedAt,
		Meta: workitem.TransitionMeta{ActorPrincipalID: actor, Reason: journeyReasonClaimed, At: decidedAt},
	})
	if err != nil {
		return decidedApproval{}, proposalWorkItemError(err)
	}
	started, err := store.Start(ctx, tx, tenantID, item.WorkItemID, claimed.ItemVersion, decidedAt,
		workitem.TransitionMeta{ActorPrincipalID: actor, Reason: journeyReasonStarted, At: decidedAt})
	if err != nil {
		return decidedApproval{}, proposalWorkItemError(err)
	}

	var completed workitem.WorkItem
	var decision intentapproval.ApprovalDecision
	var outcome frontier.NodeOutcome
	if started.Kind == workitem.KindTask {
		completed, err = store.Complete(ctx, tx, workitem.CompleteInput{
			TenantID: started.TenantID, WorkItemID: started.WorkItemID, ExpectedVersion: started.ItemVersion,
			CompletedBy: actor, CompletedOutputDigest: "sha256:" + strings.Repeat("0", 64), Now: decidedAt,
			Meta: workitem.TransitionMeta{ActorPrincipalID: actor, Reason: journeyReasonDecided, At: decidedAt},
		})
		if err != nil {
			return decidedApproval{}, proposalWorkItemError(err)
		}
		result := workflow.OutcomeRejected
		if d.Approve {
			result = workflow.OutcomeSucceeded
		}
		outcome = frontier.NodeOutcome{NodeID: completed.NodeID, Outcome: result, OutputDigest: completed.CompletedOutputDigest}
	} else {
		decision = e.approvalDecision(started, inst, revision.ProposalRevisionID, revision.MaterialDigest, d, decidedAt, actor)
		decision.Approver.Via, decision.Approver.DelegationID = candidate.Via, candidate.DelegationID
		completed, err = stepsapproval.Complete(ctx, tx, store, started, decision, decidedAt,
			workitem.TransitionMeta{ActorPrincipalID: actor, Reason: journeyReasonDecided, At: decidedAt})
		if err != nil {
			return decidedApproval{}, journeyWorkItemError(err)
		}
		outcome, err = journeyApprovalOutcome(completed, revision, decision, decidedAt)
		if err != nil {
			return decidedApproval{}, err
		}
	}
	// WF-RUN-027: the approver's own decision is recorded in intent_decision,
	// bound to the exact revision and material digest it was made against, in
	// the same transaction that completes the WorkItem. Before this, the
	// decision existed only as work_item_decision evidence hanging off the
	// human-work row, which the workflow runtime has no way to read: a
	// governance question about whether this revision was approved could only
	// be answered by trusting whoever asked. It is now a fact
	// [DurableProposalFacts] reads back.
	if started.Kind == workitem.KindApproval {
		if err := e.recordApprovalDecision(ctx, tx, principal, inst, revision, decision, decidedAt); err != nil {
			return decidedApproval{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return decidedApproval{}, fmt.Errorf("app: journey: commit the decision: %w", err)
	}
	return decidedApproval{item: completed, instance: instance, decision: decision, outcome: outcome}, nil
}

// recordApprovalDecision writes the approver's decision into intent_decision
// on the caller's own transaction, so it commits with the WorkItem completion
// or not at all.
//
// The recorded row names the routed requirement, the deciding caller the
// assignment admitted (PROMOUX-015), the approver's own reason, and the
// revision's material digest. A cell composed with no execution facts
// records nothing: it has no reader for these rows.
func (e *journeyEngine) recordApprovalDecision(
	ctx context.Context,
	tx dbport.Tx,
	principal *trust.Principal,
	inst intent.Instance,
	revision intent.ProposalRevision,
	decision intentapproval.ApprovalDecision,
	decidedAt time.Time,
) error {
	if e.svc.executionFacts == nil {
		return nil
	}
	intentUUID, err := executionIntentUUID(inst.IntentID)
	if err != nil {
		return err
	}
	outcome := intentcontrol.OutcomeApproved
	if decision.Outcome != intentapproval.OutcomeApproved {
		outcome = intentcontrol.OutcomeRejected
	}
	return executionDecision{
		TenantID:         e.svc.tenantUUID(principal.Tenant()),
		IntentID:         intentUUID,
		Revision:         simulationRevision,
		MaterialDigest:   revision.MaterialDigest.Digest,
		ControlDigest:    controlSnapshotDigest(inst.ControlSnapshots),
		RequirementID:    decision.Binding.RequirementID,
		Kind:             intentcontrol.DecisionHumanApproval,
		Outcome:          outcome,
		DecidedBy:        decision.Approver.PrincipalID,
		AuthorityRef:     decision.AuthorityDecisionRef,
		Reason:           nonEmptyReason(decision.Reason, journeyReasonDecided),
		DecidedAt:        decidedAt,
		Proposal:         &revision,
		ProposalVerifier: e.svc.digester,
		TenantUUID:       e.svc.tenantUUID,
	}.record(ctx, tx)
}

// approvalDecision mints the immutable approval decision from durable facts
// only: the routed assignment's own requirement identity and expression
// digest, the intent, the re-simulated proposal, the configured approver, the
// approver's own reason and one instant.
//
// Nothing here is invented per call, so the same work item decided at the same
// instant with the same reason always digests to the same value -- which is
// what makes the digest [workitem.Store.Complete] records a binding rather
// than a nonce.
func (e *journeyEngine) approvalDecision(
	item workitem.WorkItem,
	inst intent.Instance,
	proposalRevisionID string,
	proposalDigest digest.Reference,
	d workspace.Decision,
	decidedAt time.Time,
	approver string,
) intentapproval.ApprovalDecision {
	outcome := intentapproval.OutcomeApproved
	if !d.Approve {
		outcome = intentapproval.OutcomeRejected
	}
	resolution := item.Assignment.Resolution
	return intentapproval.ApprovalDecision{
		DecisionID: "decision:journey:" + item.WorkItemID.String(),
		Binding: intentapproval.DecisionBinding{
			RequirementID:              item.ApprovalRequirementRef,
			RequirementRevision:        resolution.RequirementRevision,
			RequirementDigest:          resolution.RequirementDigest,
			ResolutionExpressionDigest: resolution.ExpressionDigest,
			IntentID:                   inst.IntentID,
			ProposalRevisionID:         proposalRevisionID,
			ProposalDigest:             proposalDigest,
			ControlSnapshots:           inst.ControlSnapshots,
			TaskVersion:                1,
			RenderedProjectionDigest:   item.AssignmentDigest,
		},
		Outcome: outcome,
		Approver: intentapproval.ApproverReference{
			PrincipalID:          approver,
			IdentityAssuranceRef: "assurance.journey.execution-authority/v1",
			SessionRef:           "session:journey:" + item.WorkItemID.String(),
			Via:                  humanwork.SourceDirect,
		},
		AuthorityDecisionRef: "authz:journey:" + inst.IntentID,
		Reason:               d.Reason,
		DecidedAt:            values.NewInstant(decidedAt),
	}
}

// journeyApprovalOutcome is the typed step resolution the resume presents,
// derived by internal/workflow/steps/approval.Resolve from the completed
// durable row and the decision recorded on it - never assembled here.
//
// Resolve needs the continuation the node parked on, and that is rebuilt
// from durable facts alone: [routedApprovalRequirement] recompiles the
// requirement internal/platform/execution's work-item factory compiled when it
// routed the item, for the routed candidate whose compilation reproduces the
// requirement digest the assignment recorded. Resolve then re-checks every
// binding (requirement, proposal, approver, deadline, the completed-output
// digest against the decision) before it names an outcome; the output digest
// it returns is the resolution's own, which is what the driver records on the
// node execution.
func journeyApprovalOutcome(
	item workitem.WorkItem,
	revision intent.ProposalRevision,
	decision intentapproval.ApprovalDecision,
	now time.Time,
) (frontier.NodeOutcome, error) {
	requirement, err := routedApprovalRequirement(item)
	if err != nil {
		return frontier.NodeOutcome{}, fmt.Errorf("app: journey: rebuild the approval requirement: %w", err)
	}
	items := []workitem.WorkItem{item}
	continuation, err := stepsapproval.NewContinuation(item.WorkflowInstanceID, item.NodeID, revision,
		humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{requirement}}, items)
	if err != nil {
		return frontier.NodeOutcome{}, fmt.Errorf("app: journey: rebuild the approval continuation: %w", err)
	}
	resolution, err := stepsapproval.Resolve(continuation, items,
		[]intentapproval.ApprovalDecision{decision}, values.NewInstant(now),
		stepsapproval.Event{Kind: stepsapproval.EventDecisionsChanged})
	if err != nil {
		return frontier.NodeOutcome{}, fmt.Errorf("app: journey: resolve the approval: %w", err)
	}
	outcome := resolution.ToNodeOutcome(item.NodeID)
	if outcome.Failed || outcome.Outcome == "" {
		return frontier.NodeOutcome{}, fmt.Errorf(
			"app: journey: the approval resolved to no routable outcome (%q)", resolution.Outcome)
	}
	return outcome, nil
}

// journeyWorkItemError projects a human-work refusal onto the journey port.
// A refusal about the item's own state (already claimed, illegal transition,
// a binding that does not match) is a stage refusal; anything else is this
// cell failing.
func journeyWorkItemError(err error) error {
	if err == nil {
		return nil
	}
	if code := workitem.CodeOf(err); code != "" {
		return fmt.Errorf("%w: %s", workspace.ErrJourneyStage, err.Error())
	}
	return fmt.Errorf("app: journey: complete the approval: %w", err)
}
