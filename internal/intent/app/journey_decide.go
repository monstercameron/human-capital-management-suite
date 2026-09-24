package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
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
func (e *journeyEngine) Execute(ctx context.Context, intentID string) (detail workspace.JourneyDetail, retErr error) {
	defer func() {
		e.journeyEvent(ctx, "journey.workflow_started", intentID, retErr, slog.String("stage", string(detail.Summary.Stage)))
		e.publishCommitted(ctx, retErr, intentID)
	}()
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
func (e *journeyEngine) Decide(ctx context.Context, intentID string, d workspace.Decision) (detail workspace.JourneyDetail, retErr error) {
	phase := "load"
	defer func() {
		e.journeyEvent(ctx, "journey.decision_recorded", intentID, retErr,
			slog.Bool("approve", d.Approve), slog.String("stage", string(detail.Summary.Stage)), slog.String("decision_phase", phase))
		e.publishCommitted(ctx, retErr, intentID)
	}()
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
		refusal := fmt.Errorf(
			"%w: this journey's proposal has been %s and can no longer be decided",
			workspace.ErrJourneyStage, strings.ToLower(string(inst.Lifecycle.Request)))
		// WF-STEP-003: the approval left open on the abandoned proposal
		// takes its CANCELLED route rather than parking the run forever.
		if routeErr := e.routeAbandonedApproval(ctx, principal, intentID, inst, def, rec); routeErr != nil {
			return workspace.JourneyDetail{}, errors.Join(refusal, routeErr)
		}
		return workspace.JourneyDetail{}, refusal
	}
	if gateErr := e.svc.authorizeDecision(def); gateErr != nil {
		return workspace.JourneyDetail{}, journeyError(gateErr)
	}
	if e.svc.executor == nil {
		return workspace.JourneyDetail{}, fmt.Errorf(
			"%w: this cell was composed with no execution driver", workspace.ErrJourneyUnavailable)
	}

	// A current manager assignment grants case-scoped review of this one
	// proposal, not broad workforce access. Resolve that durable relationship
	// before re-simulation; completeApproval independently reloads and rechecks
	// the assignment inside the write transaction, so this read cannot grant
	// stale action authority.
	phase = "authority"
	reviewRelationships, reviewErr := e.approvalDecisionRelationships(ctx, principal, intentID, inst)
	if reviewErr != nil {
		return workspace.JourneyDetail{}, reviewErr
	}
	phase = "revalidate"
	simulated, simErr := e.resimulateDetailedWithRelationships(ctx, intentID, reviewRelationships)
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
	phase = "prepare"
	start, startErr := e.svc.executionStart(inst, artifact, journeyApprovalRef(intentID), *simulated.Revision)
	if startErr != nil {
		return workspace.JourneyDetail{}, journeyError(startErr)
	}

	phase = "decision_and_advance"
	done, decideErr := e.completeApproval(ctx, principal, inst, start, d, e.now().UTC(), "", "")
	if decideErr != nil {
		return workspace.JourneyDetail{}, journeyError(journeyDecisionError(decideErr))
	}
	if done.executed {
		phase = "consume_result"
		// WF-STEP-018: the approval kernel already advanced the workflow in
		// the decision's own transaction.
		if outcomeErr := e.svc.consumeExecutionResult(ctx, inst, def, rec, done.execution); outcomeErr != nil {
			return workspace.JourneyDetail{}, outcomeErr
		}
	} else if done.needsResume() {
		phase = "legacy_resume"
		result, resumeErr := e.svc.executor.Resume(ctx, ExecutionResumeRequest{
			Start:                   pinnedStart(start, done.instance),
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
	if done.refusal != nil {
		// WF-STEP-003: the approval was closed, and its route taken, instead
		// of the decision the caller asked for.
		return workspace.JourneyDetail{}, fmt.Errorf("%w: %w", workspace.ErrJourneyStage, done.refusal)
	}
	detail, inspectErr := e.inspectWithRelationships(ctx, intentID, reviewRelationships)
	if inspectErr != nil {
		return workspace.JourneyDetail{}, inspectErr
	}
	// HUMAN_APPROVAL can be a multi-step route (finance then manager). The
	// first approved vote persists acceptance facts, but the product action is
	// submitted only after the journey has left every approval gate.
	if d.Approve && detail.Summary.Stage != workspace.JourneyStageFinanceApproval &&
		detail.Summary.Stage != workspace.JourneyStageManagerApproval {
		submission, submitErr := e.submitApprovedProductAction(ctx, principal, inst, *simulated.Revision)
		if submitErr != nil {
			return workspace.JourneyDetail{}, submitErr
		}
		detail.Submission = submission
	}
	return detail, nil
}

// submitApprovedProductAction runs after completeApproval has committed its
// decision transaction. The action itself is resolved from the durable
// acceptance row; no caller supplied action identity, actor, or timestamp is
// trusted here. The in-memory registry serializes exact concurrent retries
// and returns the first retained submission record.
func (e *journeyEngine) submitApprovedProductAction(
	ctx context.Context, principal *trust.Principal, inst intent.Instance, revision intent.ProposalRevision,
) (*workspace.JourneySubmissionRecord, error) {
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	accepted, err := (DurableProposalFacts{}).ResolveAcceptedAction(ctx, tx, AcceptedActionLookup{
		Tenant: principal.Tenant(), TenantID: e.svc.tenantUUID(principal.Tenant()),
		IntentID: inst.IntentID, ActionID: AcceptedIntentExecutionActionID,
		ProposalRevisionID: revision.ProposalRevisionID,
		ProposalDigest:     revision.MaterialDigest.Digest,
		IdempotencyKey:     acceptedExecutionIdempotencyKey(inst.IntentID, revision.ProposalRevisionID),
	})
	if err != nil {
		return nil, fmt.Errorf("app: journey: resolve committed accepted action: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("app: journey: commit accepted action read: %w", err)
	}
	record, _, err := e.svc.submitAcceptedProductAction(accepted)
	if err != nil {
		return nil, err
	}
	return &workspace.JourneySubmissionRecord{
		IntentID: record.IntentID, ProposalRevisionID: record.ProposalRevisionID,
		ProposalDigest: record.ProposalDigest, MaterialDigest: record.MaterialDigest,
		SubmittedBy: record.SubmittedBy, SubmittedAt: record.SubmittedAt.Time(),
		IdempotencyKey: record.IdempotencyKey, Digest: record.Digest,
	}, nil
}

func (e *journeyEngine) approvalDecisionRelationships(
	ctx context.Context, principal *trust.Principal, intentID string, inst intent.Instance,
) ([]authz.RelationshipFact, error) {
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, record, executed, err := e.readExecutedRecordForIntent(ctx, tx, principal, intentID)
	if err != nil || !executed {
		return nil, err
	}
	worker := values.EntityRef{Tenant: principal.Tenant(), Kind: "worker"}
	for _, subject := range inst.Subjects {
		if subject.Kind == "EMPLOYMENT" {
			worker.Id = subject.SubjectID
			break
		}
	}
	if err := worker.Validate(); err != nil {
		return nil, fmt.Errorf("%w: promotion worker subject: %v", workspace.ErrJourneyUnknown, err)
	}
	return approvalReviewRelationships(principal, worker, inst.CreatedAt, record.items)
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
	// refusal is set when the approval was closed without the caller's
	// decision (WF-STEP-003: EXPIRED or INVALIDATED). The route is still
	// resumed; the caller then reports refusal instead of success.
	refusal error
	// settled reports that the approval kernel (WF-STEP-018) recorded the
	// decision and took any advancement in one transaction: nothing is left
	// to resume. executed reports that execution holds that advancement's
	// result; a settled vote short of quorum has none.
	settled  bool
	executed bool
}

// needsResume distinguishes a completed decision from a completed workflow
// step for the paths that still commit before the driver advances (a TASK
// completion and the EXPIRED and CANCELLED closures): a crash in that interval
// must not make a replay strand the frontier. Once the instance has moved past
// this node, the same decision is a pure replay. A kernel-settled decision
// never needs a resume.
func (d decidedApproval) needsResume() bool {
	if d.settled {
		return false
	}
	if !d.replayed {
		return true
	}
	if d.instance.RuntimeStatus.Terminal() {
		return false
	}
	for _, nodeID := range d.instance.CurrentNodeIDs {
		if nodeID == d.item.NodeID {
			return true
		}
	}
	return false
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
	return e.completeApproval(ctx, principal, inst, start, decision, e.now().UTC(), req.RequirementID, req.RenderedProjectionDigest)
}

// completeApproval decides the journey's routed WorkItem as the caller.
//
// It reads the durable record in one tenant-scoped transaction and applies
// the journey's own admission (membership, initiator and sibling separation,
// the routed deadline, the WF-STEP-003 authority recheck). An approval
// decision is then made through the generic approval kernel
// ([journeyEngine.voteThroughKernel], WF-STEP-018): the claim, the
// completion, both decision rows and the workflow advancement commit in one
// transaction or not at all. A stale authority is routed INVALIDATED through
// the same kernel. The reapproval TASK and the EXPIRED closure still commit
// here and are resumed by the caller.
func (e *journeyEngine) completeApproval(
	ctx context.Context,
	principal *trust.Principal,
	inst intent.Instance,
	start runtime.StartRequest,
	d workspace.Decision,
	decidedAt time.Time,
	expectedRequirementID string,
	expectedProjectionDigest string,
) (out decidedApproval, retErr error) {
	phase := "load"
	defer func() {
		if retErr != nil {
			e.journeyEvent(ctx, "journey.approval_failed", inst.IntentID, retErr, slog.String("approval_phase", phase))
		}
	}()
	revision := start.Proposal.Revision
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
	// Every continuation below advances the plan this instance pinned.
	start = pinnedStart(start, instance)
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
			replayed := decidedApproval{item: completed, instance: instance, decision: existing, replayed: true}
			if !replayed.needsResume() {
				return replayed, nil
			}
			// The vote committed but its node never advanced (a decision
			// recorded before the atomic kernel, or a vote still short of
			// quorum): the kernel resolves it again from the durable record
			// and writes no second vote.
			_ = tx.Rollback(ctx)
			return e.voteThroughKernel(ctx, journeyVote{
				principal: principal, inst: inst, start: start, instance: instance, item: completed,
				decision: existing, at: existing.DecidedAt.Time(), replay: true,
			})
		}
		recovered, found, recoverErr := e.recoverClosedApproval(ctx, tx, tenantID, instance, items, revision, principal.Subject())
		if recoverErr != nil || found {
			return recovered, recoverErr
		}
		return decidedApproval{}, fmt.Errorf("%w: this promotion has no open approval to decide", ErrProposalDecisionStage)
	}
	if expectedRequirementID != "" && item.ApprovalRequirementRef != expectedRequirementID {
		return decidedApproval{}, ErrProposalDecisionStale
	}
	if expectedProjectionDigest != "" && item.AssignmentDigest != expectedProjectionDigest {
		return decidedApproval{}, ErrProposalDecisionStale
	}
	actor := principal.Subject()
	// Resolve membership and separation first so a non-member produces the
	// public denial, not an internal stale-authority diagnostic.
	phase = "membership"
	candidate, err := journeyDecider(items, item, inst, actor, decidedAt)
	if err != nil {
		return decidedApproval{}, err
	}
	// Current authority is checked before claim/start/complete. The WorkItem
	// assignment proves the routed authority class and the verified principal's
	// credential proves the actor is still current at this instant; neither is
	// inferred from a button payload. The history check prevents one principal
	// from satisfying both finance and current-manager approval classes.
	phase = "authority_binding"
	if err := e.validateRoutedJourneyApprover(ctx, tx, principal, inst, item, candidate, revision, decidedAt); err != nil {
		return decidedApproval{}, err
	}
	phase = "approval_history"
	if err := ValidatePromotionApprovalHistory(items, item, actor); err != nil {
		return decidedApproval{}, err
	}
	// WF-STEP-003: a decision at or after the routed deadline cannot approve.
	// The requirement has expired, which is a fact about the item, not about
	// the click: the item is durably EXPIRED and the step resolves to its
	// EXPIRED route in this transaction, and the approver is told why after
	// the driver has taken that route.
	if !decidedAt.Before(item.DeadlineAt) {
		return e.closeApproval(ctx, tx, instance, revision, item, actor, decidedAt, approvalClosureExpired,
			fmt.Sprintf("the approval deadline %s has passed", item.DeadlineAt.UTC().Format(time.RFC3339)))
	}
	// WF-STEP-003: the authority the item was routed under is re-resolved
	// from current durable facts. An approver who no longer holds it (the
	// subject's manager changed, the finance authority moved, the role was
	// revoked) cannot approve: the item is durably closed and the step takes
	// its INVALIDATED route, which the compiled plan sends to reapproval by a
	// fresh proposal.
	phase = "current_authority"
	stale, err := e.recheckApprovalAuthority(ctx, tx, principal, inst, item, candidate, revision, decidedAt)
	if err != nil {
		return decidedApproval{}, err
	}
	if stale != nil {
		// WF-STEP-018: AUTHORITY_REVOKED is a declared invalidator of the
		// promotion requirements; the kernel closes the item and routes
		// INVALIDATED in one transaction.
		_ = tx.Rollback(ctx)
		return e.invalidateThroughKernel(ctx, start, instance, item, actor, decidedAt, stale.Error())
	}
	if item.Kind == workitem.KindApproval {
		phase = "kernel_vote"
		decision := e.approvalDecision(item, inst, revision.ProposalRevisionID, revision.MaterialDigest, d, decidedAt, actor)
		decision.Approver.Via, decision.Approver.DelegationID = candidate.Via, candidate.DelegationID
		_ = tx.Rollback(ctx)
		return e.voteThroughKernel(ctx, journeyVote{
			principal: principal, inst: inst, start: start, instance: instance, item: item,
			candidate: candidate, decision: decision, at: decidedAt,
		})
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

	// The reapproval TASK is not an approval requirement: it completes here
	// and the caller resumes the driver from it.
	completed, err := store.Complete(ctx, tx, workitem.CompleteInput{
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
	outcome := frontier.NodeOutcome{NodeID: completed.NodeID, Outcome: result, OutputDigest: completed.CompletedOutputDigest}
	if err := tx.Commit(ctx); err != nil {
		return decidedApproval{}, fmt.Errorf("app: journey: commit the decision: %w", err)
	}
	return decidedApproval{item: completed, instance: instance, outcome: outcome}, nil
}

// recordApprovalDecision writes the approver's decision into intent_decision
// on the caller's own transaction, so it commits with the WorkItem completion
// or not at all.
//
// WF-RUN-027: the decision is bound to the exact revision and material digest
// it was made against, so whether this revision was approved is a fact
// [DurableProposalFacts] reads back rather than evidence only the human-work
// row holds.
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
	return resolveJourneyApproval(item, revision, []intentapproval.ApprovalDecision{decision}, now, stepsapproval.Event{})
}

// resolveJourneyApproval is [journeyApprovalOutcome] over any durable item
// state. decisions is the one decision a completed item records, or none for
// an item closed without a completion (EXPIRED, or CANCELLED by an authority
// recheck); event is the zero value except for the INVALIDATED event, with its
// reason, an authority recheck raised.
func resolveJourneyApproval(
	item workitem.WorkItem,
	revision intent.ProposalRevision,
	decisions []intentapproval.ApprovalDecision,
	now time.Time,
	event stepsapproval.Event,
) (frontier.NodeOutcome, error) {
	if event.Kind == "" {
		event.Kind = stepsapproval.EventDecisionsChanged
	}
	continuation, _, err := journeyContinuation(item, revision)
	if err != nil {
		return frontier.NodeOutcome{}, err
	}
	resolution, err := stepsapproval.Resolve(continuation, []workitem.WorkItem{item}, decisions, values.NewInstant(now), event)
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
