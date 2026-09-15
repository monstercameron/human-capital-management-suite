package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var (
	// ErrApprovalAuthorityDenied reports that the mandatory decision-time
	// authority check did not authorize the exact decision and durable item.
	ErrApprovalAuthorityDenied = errors.New("workflow execute: current approval authority denied")
	// ErrApprovalCompletionConflict reports a completed WorkItem whose stored
	// decision or workflow result differs from the submitted immutable decision.
	ErrApprovalCompletionConflict = errors.New("workflow execute: approval completion conflict")
	// ErrApprovalResolutionPending protects the bounded one-item prototype from
	// committing a partial quorum without the matching workflow advancement.
	ErrApprovalResolutionPending = errors.New("workflow execute: approval resolution still awaits work")
)

// CurrentApprovalAuthorityRequest is the complete server-held context checked
// immediately before an approval completion. Implementations may read current
// session, separation-of-duties, delegation and directory state through Tx.
type CurrentApprovalAuthorityRequest struct {
	TenantID     uuid.UUID
	Item         workitem.WorkItem
	Decision     intentapproval.ApprovalDecision
	Proposal     runtime.ProposalBinding
	Continuation stepapproval.Continuation
	CheckedAt    time.Time
}

// CurrentApprovalAuthorityDecision is the current authorization verdict and
// its durable evidence reference. An allowed decision without a reference is
// refused because it cannot be audited.
type CurrentApprovalAuthorityDecision struct {
	Allowed     bool
	DecisionRef string
	Reason      string
}

// CurrentApprovalAuthority rechecks authority inside the same transaction as
// WorkItem completion and runtime advancement.
type CurrentApprovalAuthority interface {
	Recheck(context.Context, workitem.Executor, CurrentApprovalAuthorityRequest) (CurrentApprovalAuthorityDecision, error)
}

// ApprovalCompletionRequest carries immutable approval evidence, never a
// caller-selected NodeOutcome. PriorDecisions are needed only by a multi-item
// continuation; every one must match a durable completed WorkItem in Resolve.
type ApprovalCompletionRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	WorkItemID              uuid.UUID
	ExpectedWorkItemVersion int64
	Continuation            stepapproval.Continuation
	Decision                intentapproval.ApprovalDecision
	PriorDecisions          []intentapproval.ApprovalDecision
	RecordedAt              time.Time
	Meta                    workitem.TransitionMeta
	Authority               CurrentApprovalAuthority
}

// ApprovalCompletionResult returns the server-derived resolution and the
// ordinary driver result. Replay means no WorkItem or runtime write was issued.
type ApprovalCompletionResult struct {
	Result
	CompletedItem workitem.WorkItem
	Resolution    stepapproval.Resolution
	AuthorityRef  string
	Replay        bool
}

// CompleteApproval performs the WORK-006 prototype boundary. The durable item
// is loaded, authority is rechecked, the item is completed, Resolve derives the
// only NodeOutcome runtime sees, and Advance persists its signal in one tenant
// transaction. READY successors are drained only after that transaction commits.
func (d *Driver) CompleteApproval(ctx context.Context, req ApprovalCompletionRequest) (ret0 ApprovalCompletionResult, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.complete_approval", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	selection, at, err := validateApprovalCompletionRequest(ctx, req)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	ctx, release, err := d.acquireInstanceLease(ctx, req.Start.TenantID, req.InstanceID)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	defer func() { retErr = releasing(retErr, release) }()
	tx, err := d.opts.DB.Begin(ctx)
	if err != nil {
		return ApprovalCompletionResult{}, fmt.Errorf("workflow execute: begin approval completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, req.Start.TenantID); err != nil {
		return ApprovalCompletionResult{}, err
	}

	store := workitem.Store{}
	item, err := store.Load(ctx, tx, req.Start.TenantID, req.WorkItemID)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	if err := validateApprovalBindings(req, selection, item); err != nil {
		return ApprovalCompletionResult{}, err
	}

	if item.Status == workitem.StatusCompleted {
		return d.replayApprovalCompletion(ctx, tx, req, selection, item, at)
	}
	if item.ItemVersion != req.ExpectedWorkItemVersion {
		return ApprovalCompletionResult{}, fmt.Errorf("%w: expected WorkItem version %d, stored %d",
			ErrApprovalCompletionConflict, req.ExpectedWorkItemVersion, item.ItemVersion)
	}

	authority, err := req.Authority.Recheck(ctx, tx, CurrentApprovalAuthorityRequest{
		TenantID: req.Start.TenantID, Item: item, Decision: req.Decision,
		Proposal: req.Start.Proposal, Continuation: req.Continuation, CheckedAt: at,
	})
	if err != nil {
		return ApprovalCompletionResult{}, fmt.Errorf("workflow execute: recheck approval authority: %w", err)
	}
	if !authority.Allowed || authority.DecisionRef == "" {
		return ApprovalCompletionResult{}, fmt.Errorf("%w: %s", ErrApprovalAuthorityDenied, authority.Reason)
	}
	if authority.DecisionRef != req.Decision.AuthorityDecisionRef {
		return ApprovalCompletionResult{}, fmt.Errorf("%w: current authority evidence does not match the decision binding", ErrApprovalAuthorityDenied)
	}

	completed, err := stepapproval.Complete(ctx, tx, store, item, req.Decision, at, req.Meta)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	items, err := store.ListForInstance(ctx, tx, req.Start.TenantID, req.InstanceID)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	resolution, err := resolveCompletedApproval(req, items, at)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	if resolution.Outcome == "" {
		return ApprovalCompletionResult{}, ErrApprovalResolutionPending
	}

	outcome := resolution.ToNodeOutcome(item.NodeID)
	refs := runtime.GovernanceRefs{
		AuthorizationDecisionID: authority.DecisionRef,
		DecisionID:              req.Decision.DecisionID,
		HumanTaskID:             item.WorkItemID.String(),
		ProposalRef:             req.Start.Proposal.Revision.MaterialDigest.Digest,
	}
	run := runContext{start: req.Start, selection: selection, instanceID: req.InstanceID}
	sink := d.newContinuationSink(tx, run)
	// The approval node may be on its second or later activation (a
	// re-approval routed back to the same gate); the advancement must name
	// the attempt that is actually open, never the first one. Only a plan
	// that declares a cycle can re-enter a node, so only such a plan pays
	// the lookup.
	attempt := 1
	if len(selection.Plan.Limits.DeclaredCycles) > 0 {
		executions, err := (runtime.Store{}).LoadNodeExecutions(ctx, tx, req.Start.TenantID, req.InstanceID)
		if err != nil {
			return ApprovalCompletionResult{}, fmt.Errorf("workflow execute: load node executions for approval completion: %w", err)
		}
		attempt = highestAttempt(executions, item.NodeID)
	}
	advCtx, advSpan := d.opts.Instrumentation.StartAdvanceSpan(ctx, SpanAttributes{
		InstanceID: req.InstanceID.String(), NodeID: item.NodeID, Attempt: attempt,
	})
	advReq := runtime.AdvanceRequest{
		TenantID: req.Start.TenantID, InstanceID: req.InstanceID,
		ExpectedInstanceVersion: req.ExpectedInstanceVersion, Attempt: attempt,
		Plan: selection.Plan, Outcome: outcome, Refs: refs,
		RecordedAt: at, Sink: sink, TraceID: d.opts.Instrumentation.TraceID(advCtx),
	}
	if causalSpan, ok := advSpan.(CausalSpan); ok {
		nodeExecutionID := runtime.NodeExecutionID(req.Start.TenantID, req.InstanceID, item.NodeID, attempt).String()
		advReq.Causal = causalSpan.CausalMetadata(CausalIdentity{
			CorrelationID: req.Start.CorrelationID, CausationID: nodeExecutionID,
			LogicalOperationID: req.InstanceID.String(), AttemptID: nodeExecutionID,
			ExpiresAt: at.Add(24 * time.Hour),
		})
	}
	advanced, err := d.advance(advCtx, tx, advReq)
	if err != nil {
		advSpan.End(OutcomeFailure, err)
		return ApprovalCompletionResult{}, err
	}
	if err := tx.Commit(advCtx); err != nil {
		advSpan.End(OutcomeFailure, err)
		return ApprovalCompletionResult{}, fmt.Errorf("workflow execute: commit approval completion: %w", err)
	}
	advOutcome := OutcomeSuccess
	if !advanced.Complete && len(advanced.Continuations) > 0 {
		advOutcome = OutcomeParked
	}
	advSpan.End(advOutcome, nil)

	base := Result{
		Advances: []runtime.AdvanceReceipt{advanced}, WorkItems: append([]workitem.WorkItem(nil), sink.created...),
		InstanceVersion: advanced.NewInstanceVersion, Frontier: append([]string(nil), advanced.Frontier...),
	}
	result, err := d.finishApprovalDrain(ctx, run, base, advanced)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	return ApprovalCompletionResult{
		Result: result, CompletedItem: completed, Resolution: resolution,
		AuthorityRef: authority.DecisionRef,
	}, nil
}

func validateApprovalCompletionRequest(ctx context.Context, req ApprovalCompletionRequest) (runtime.WorkflowSelection, time.Time, error) {
	if req.Start.Resolver == nil || req.Start.Versions == nil || req.Authority == nil {
		return runtime.WorkflowSelection{}, time.Time{}, invalid("approval completion requires resolver, version store and current-authority port")
	}
	if req.Start.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.WorkItemID == uuid.Nil ||
		req.ExpectedInstanceVersion < 1 || req.ExpectedWorkItemVersion < 1 {
		return runtime.WorkflowSelection{}, time.Time{}, invalid("approval completion requires tenant, instance, WorkItem and positive versions")
	}
	at := req.RecordedAt.UTC()
	if req.RecordedAt.IsZero() {
		return runtime.WorkflowSelection{}, time.Time{}, invalid("approval completion requires RecordedAt")
	}
	selection, err := req.Start.Resolver.ResolveWorkflow(ctx, req.Start)
	if err != nil {
		return runtime.WorkflowSelection{}, time.Time{}, fmt.Errorf("workflow execute: resolve approval workflow: %w", err)
	}
	if selection.Plan == nil || selection.WorkflowID == "" {
		return runtime.WorkflowSelection{}, time.Time{}, invalid("WorkflowResolver returned no workflow id or plan for approval completion")
	}
	published, err := version.Resolve(req.Start.Versions, selection.WorkflowID, selection.Pin)
	if err != nil {
		return runtime.WorkflowSelection{}, time.Time{}, fmt.Errorf("workflow execute: resolve approval version: %w", err)
	}
	if published.Status != version.StatusActive || published.CompiledPlanDigest != selection.Plan.Digest() {
		return runtime.WorkflowSelection{}, time.Time{}, invalid("approval plan is not the exact active published version")
	}
	return selection, at, nil
}

func validateApprovalBindings(req ApprovalCompletionRequest, selection runtime.WorkflowSelection, item workitem.WorkItem) error {
	if item.TenantID != req.Start.TenantID || item.WorkflowInstanceID != req.InstanceID || item.WorkItemID != req.WorkItemID {
		return fmt.Errorf("%w: WorkItem belongs to another tenant or instance", ErrApprovalCompletionConflict)
	}
	if item.Kind != workitem.KindApproval || item.NodeID != req.Continuation.NodeID || req.Continuation.WorkflowInstanceID != req.InstanceID {
		return fmt.Errorf("%w: WorkItem and continuation do not name the same approval node", ErrApprovalCompletionConflict)
	}
	node, ok := selection.Plan.Node(item.NodeID)
	if !ok || node.Type != workflow.StepApproval {
		return fmt.Errorf("%w: node %s is not APPROVAL in the pinned plan", ErrApprovalCompletionConflict, item.NodeID)
	}
	proposal := req.Start.Proposal.Revision
	if proposal.ProposalRevisionID == "" || proposal.ProposalRevisionID != req.Continuation.ProposalRevisionID ||
		!sameDigestReference(proposal.MaterialDigest, req.Continuation.ProposalDigest) || item.ProposalRef != proposal.MaterialDigest.Digest {
		return fmt.Errorf("%w: stale or mismatched proposal revision", ErrApprovalCompletionConflict)
	}
	if item.CorrelationID != req.Start.CorrelationID || !sameStrings(item.SubjectRefs, req.Start.BusinessSubjectRefs) {
		return fmt.Errorf("%w: WorkItem context differs from the workflow", ErrApprovalCompletionConflict)
	}
	found := false
	for _, requirement := range req.Continuation.Requirements {
		for _, id := range requirement.WorkItemIDs {
			found = found || id == item.WorkItemID
		}
	}
	if !found {
		return fmt.Errorf("%w: continuation does not admit WorkItem %s", ErrApprovalCompletionConflict, item.WorkItemID)
	}
	return nil
}

func resolveCompletedApproval(req ApprovalCompletionRequest, items []workitem.WorkItem, at time.Time) (stepapproval.Resolution, error) {
	decisions := append([]intentapproval.ApprovalDecision(nil), req.PriorDecisions...)
	decisions = append(decisions, req.Decision)
	return stepapproval.Resolve(req.Continuation, items, decisions, values.NewInstant(at), stepapproval.Event{Kind: stepapproval.EventDecisionsChanged})
}

func (d *Driver) replayApprovalCompletion(
	ctx context.Context, tx dbport.Tx, req ApprovalCompletionRequest, selection runtime.WorkflowSelection,
	item workitem.WorkItem, at time.Time,
) (ApprovalCompletionResult, error) {
	if item.CompletedOutputDigest != req.Decision.Digest() {
		return ApprovalCompletionResult{}, fmt.Errorf("%w: WorkItem stores another decision", ErrApprovalCompletionConflict)
	}
	items, err := workitem.Store{}.ListForInstance(ctx, tx, req.Start.TenantID, req.InstanceID)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	resolution, err := resolveCompletedApproval(req, items, at)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	if resolution.Outcome == "" {
		return ApprovalCompletionResult{}, ErrApprovalResolutionPending
	}
	node, err := runtime.Store{}.LoadNodeExecution(ctx, tx, req.Start.TenantID, req.InstanceID, item.NodeID, 1)
	if err != nil || node.Status != runtime.NodeSucceeded || node.OutputArtifactRef != resolution.Digest {
		return ApprovalCompletionResult{}, fmt.Errorf("%w: completed WorkItem has no matching workflow advancement", ErrApprovalCompletionConflict)
	}
	inst, err := runtime.Store{}.LoadInstance(ctx, tx, req.Start.TenantID, req.InstanceID)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	ready := make([]string, 0, len(inst.CurrentNodeIDs))
	for _, id := range inst.CurrentNodeIDs {
		n, loadErr := runtime.Store{}.LoadNodeExecution(ctx, tx, req.Start.TenantID, req.InstanceID, id, 1)
		if loadErr != nil {
			return ApprovalCompletionResult{}, loadErr
		}
		if n.Status == runtime.NodeReady {
			ready = append(ready, id)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ApprovalCompletionResult{}, fmt.Errorf("workflow execute: commit approval replay read: %w", err)
	}
	base := Result{InstanceVersion: inst.InstanceVersion, Frontier: append([]string(nil), inst.CurrentNodeIDs...)}
	if inst.RuntimeStatus.Terminal() {
		base.Status = StatusComplete
	} else if len(ready) > 0 {
		base, err = d.drainReady(ctx, runContext{start: req.Start, selection: selection, instanceID: req.InstanceID}, base, ready)
		if err != nil {
			return ApprovalCompletionResult{}, err
		}
	} else {
		base.Status = StatusParked
	}
	return ApprovalCompletionResult{
		Result: base, CompletedItem: item, Resolution: resolution,
		AuthorityRef: node.Refs.AuthorizationDecisionID, Replay: true,
	}, nil
}

func (d *Driver) newContinuationSink(tx dbport.Tx, run runContext) *continuationSink {
	return &continuationSink{
		tx:      tx,
		durable: runtime.ContinuationStore{}, factory: d.opts.WorkItems, terminal: d.opts.Terminal,
		repair: d.opts.Repair,
		guard:  d.opts.Guard, policy: d.opts.Retention, workflowID: run.selection.WorkflowID,
		planDigest: run.selection.Plan.Digest(), proposal: run.start.Proposal, cellID: run.start.CellID,
		correlationID: run.start.CorrelationID, startKey: run.start.StartIdempotencyKey,
		subjectRefs: append([]string(nil), run.start.BusinessSubjectRefs...),
		// OBS-023/OBS-024: same ports the ordinary advanceOnce path wires;
		// a nil Instrumentation/Evidence here (a Driver not built through
		// New) is tolerated by continuationSink.Complete's own defensive
		// nil check.
		instrumentation: d.opts.Instrumentation, evidence: d.opts.Evidence,
	}
}

func (d *Driver) finishApprovalDrain(ctx context.Context, run runContext, result Result, advanced runtime.AdvanceReceipt) (Result, error) {
	if advanced.Complete {
		result.Status = StatusComplete
		return result, nil
	}
	ready, parked := readyAndParked(advanced.Continuations)
	if parked {
		result.Status = StatusParked
		return result, nil
	}
	if len(ready) == 0 {
		return Result{}, fmt.Errorf("%w: completed approval has no READY continuation", ErrNoProgress)
	}
	return d.drainReady(ctx, run, result, ready)
}

func sameDigestReference(a, b digest.Reference) bool {
	return a.ProfileID == b.ProfileID && a.ProfileVersion == b.ProfileVersion &&
		a.SchemaID == b.SchemaID && a.SchemaVersion == b.SchemaVersion &&
		a.AlgorithmID == b.AlgorithmID && a.CanonicalLength == b.CanonicalLength &&
		a.Digest == b.Digest && a.ScopeBindingDigest == b.ScopeBindingDigest &&
		sameOptionalString(a.CanonicalBytesArtifactRef, b.CanonicalBytesArtifactRef) &&
		sameOptionalString(a.IntentID, b.IntentID) &&
		sameOptionalString(a.ProposalRevisionID, b.ProposalRevisionID) &&
		sameOptionalString(a.MaterialProfileRef, b.MaterialProfileRef)
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// highestAttempt returns the highest attempt recorded for nodeID on the
// instance, or 1 when none is recorded, so a completion addresses the open
// activation of a node that has been routed back to more than once.
func highestAttempt(rows []runtime.NodeExecution, nodeID string) int {
	highest := 0
	for _, row := range rows {
		if row.NodeID == nodeID && row.Attempt > highest {
			highest = row.Attempt
		}
	}
	if highest == 0 {
		return 1
	}
	return highest
}
