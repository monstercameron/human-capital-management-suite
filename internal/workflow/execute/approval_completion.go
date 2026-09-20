package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// WF-STEP-018: the generic approval kernel.
//
// [Driver.CompleteApproval] records one vote and, when that vote resolves the
// APPROVAL node, advances the node in the same transaction:
//
//   - every approval work item of the proposal is locked first
//     ([workitem.Store.LockApprovalSiblings]), so concurrent votes serialize and
//     a second final vote observes the first one's resolution;
//   - the vote is refused, with nothing written, for a stale item or node, a
//     duplicate approver on a distinct-approver requirement, a separation of
//     duties conflict, or a denied current-authority recheck;
//   - the WorkItem completion, its work_item_decision row and the caller's own
//     Record evidence are written, then [stepapproval.Resolve] evaluates the
//     continuation over the durable decisions of every completed slot;
//   - a vote short of quorum commits alone and the node keeps WAITING; a vote
//     that resolves the node cancels the slots it no longer needs and runs
//     runtime.Advance and the continuation sink before the one commit, so a
//     failure anywhere after the decision leaves neither the decision nor the
//     advancement.
//
// The advancement runs through the driver's ordinary advanceOnce, so a served
// driver's lease fence, version quarantine, currency guard and retry policy
// apply to an approval exactly as they do to any other node.

var (
	// ErrApprovalAuthorityDenied reports that the mandatory decision-time
	// authority check did not authorize the exact decision and durable item.
	ErrApprovalAuthorityDenied = errors.New("workflow execute: current approval authority denied")
	// ErrApprovalCompletionConflict reports a vote that cannot be applied to the
	// durable record as it stands: a stale WorkItem or instance, a closed slot,
	// an approval node that is no longer waiting, or a completed WorkItem whose
	// stored decision differs from the submitted one.
	ErrApprovalCompletionConflict = errors.New("workflow execute: approval completion conflict")
)

// errApprovalAlreadyAdvanced marks a replayed vote whose node has already been
// advanced; the vote transaction wrote nothing and is rolled back.
var errApprovalAlreadyAdvanced = errors.New("workflow execute: approval already advanced")

// errCommitWithoutAdvance asks advanceOnce to commit the inputs' own writes
// without an advancement: a durable vote that has not reached quorum.
var errCommitWithoutAdvance = errors.New("workflow execute: commit without advance")

// Transition reason the kernel records on a slot it cancels because the node
// it belongs to resolved without it.
const reasonApprovalResolved = "workflow.approval.resolved"

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

// ApprovalCompletionRequest carries one immutable vote, never a caller-selected
// NodeOutcome. The decisions of other slots are read from durable storage.
type ApprovalCompletionRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	WorkItemID              uuid.UUID
	ExpectedWorkItemVersion int64
	Continuation            stepapproval.Continuation
	Decision                intentapproval.ApprovalDecision
	RecordedAt              time.Time
	Meta                    workitem.TransitionMeta
	Authority               CurrentApprovalAuthority
	// Prepare, when set, moves the loaded open WorkItem into the state
	// completion accepts (a surface that decides on the approver's behalf
	// claims and starts it) inside the vote transaction, after the authority
	// recheck. It must return the item it wrote.
	Prepare func(context.Context, workitem.Executor, workitem.WorkItem) (workitem.WorkItem, error)
	// Record, when set, appends the caller's own evidence of the vote (for
	// example the intent decision row) inside the vote transaction, after the
	// WorkItem completion and before the resolution.
	Record func(context.Context, workitem.Executor, workitem.WorkItem) error
}

// ApprovalCompletionResult returns the server-derived resolution and the
// ordinary driver result.
type ApprovalCompletionResult struct {
	Result
	CompletedItem workitem.WorkItem
	Resolution    stepapproval.Resolution
	AuthorityRef  string
	// Pending reports a durable vote that did not resolve the node: its
	// requirement has not reached quorum and the node is still WAITING.
	Pending bool
	// Replay reports that the WorkItem already recorded this exact decision and
	// no vote was written by this call.
	Replay bool
	// Closed are the open slots the resolution cancelled.
	Closed []workitem.WorkItem
}

// approvalVote is what the vote transaction learned, read by CompleteApproval
// after advanceOnce returns.
type approvalVote struct {
	item         workitem.WorkItem
	completed    workitem.WorkItem
	resolution   stepapproval.Resolution
	authorityRef string
	replay       bool
	closed       []workitem.WorkItem
}

// CompleteApproval records one vote and advances the APPROVAL node when the
// vote resolves it, in one tenant transaction. READY successors are drained
// only after that transaction commits.
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

	run := runContext{
		start: req.Start, selection: selection, instanceID: req.InstanceID,
		traceID: d.opts.Instrumentation.TraceID(ctx),
	}
	var vote approvalVote
	// OBS-024: the same APPROVAL_COMPLETED evidence Resume records, on the
	// advance transaction itself so it commits beside the outcome.
	advanced, created, evidenceIDs, timers, err := d.advanceOnce(ctx, run, req.ExpectedInstanceVersion, at, 1,
		func(ctx context.Context, ex runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error) {
			outcome, refs, voteErr := d.voteApproval(ctx, ex, req, selection, at, &vote)
			return outcome, refs, nil, voteErr
		},
		func(runtime.AdvanceReceipt) (string, string, bool) {
			return EvidenceKindApprovalCompleted, vote.completed.WorkItemID.String(), true
		})
	switch {
	case errors.Is(err, errApprovalAlreadyAdvanced):
		return d.replayApprovalCompletion(ctx, run, vote.item)
	case errors.Is(err, errCommitWithoutAdvance):
		return ApprovalCompletionResult{
			Result:        Result{Status: StatusParked, InstanceVersion: req.ExpectedInstanceVersion},
			CompletedItem: vote.completed, Resolution: vote.resolution, AuthorityRef: vote.authorityRef,
			Pending: true, Replay: vote.replay,
		}, nil
	case err != nil:
		settled := d.settlePause(ctx, run, at, err)
		if paused, ok := pausedResult(settled, Result{}); ok {
			return ApprovalCompletionResult{Result: paused}, nil
		}
		return ApprovalCompletionResult{}, settled
	}

	base := Result{
		Advances: []runtime.AdvanceReceipt{advanced}, WorkItems: created, Timers: timers,
		InstanceVersion: advanced.NewInstanceVersion, Frontier: append([]string(nil), advanced.Frontier...),
		EvidenceIDs: evidenceIDs,
	}
	result, err := d.finishApprovalDrain(ctx, run, base, advanced)
	if err != nil {
		return ApprovalCompletionResult{}, err
	}
	return ApprovalCompletionResult{
		Result: result, CompletedItem: vote.completed, Resolution: vote.resolution,
		AuthorityRef: vote.authorityRef, Replay: vote.replay, Closed: vote.closed,
	}, nil
}

// voteApproval is the body of the vote transaction. It returns the node
// outcome to advance, errCommitWithoutAdvance for a durable vote short of
// quorum, or errApprovalAlreadyAdvanced for a replay of a settled vote.
func (d *Driver) voteApproval(
	ctx context.Context, ex runtime.Executor, req ApprovalCompletionRequest, selection runtime.WorkflowSelection,
	at time.Time, vote *approvalVote,
) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	tenantID := req.Start.TenantID
	store := workitem.Store{}
	if _, err := store.LockApprovalSiblings(ctx, ex, tenantID, req.Start.Proposal.Revision.MaterialDigest.Digest); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	item, err := store.Load(ctx, ex, tenantID, req.WorkItemID)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	vote.item = item
	if err := validateApprovalBindings(req, selection, item); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	node, err := openApprovalNode(ctx, ex, tenantID, req.InstanceID, item.NodeID)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}

	authorityRef := req.Decision.AuthorityDecisionRef
	if item.Status == workitem.StatusCompleted {
		if item.CompletedOutputDigest != req.Decision.Digest() {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("%w: WorkItem stores another decision", ErrApprovalCompletionConflict)
		}
		vote.replay, vote.completed = true, item
		if node.Status != runtime.NodeWaiting {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errApprovalAlreadyAdvanced
		}
		// The vote committed but its node did not advance (it was short of
		// quorum, or it predates the atomic kernel): resolve again from the
		// durable record without writing a second vote.
	} else {
		if node.Status != runtime.NodeWaiting {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("%w: approval node %s is %s, not WAITING", ErrApprovalCompletionConflict, item.NodeID, node.Status)
		}
		if item.Status.Terminal() {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("%w: WorkItem %s is %s", ErrApprovalCompletionConflict, item.WorkItemID, item.Status)
		}
		if item.ItemVersion != req.ExpectedWorkItemVersion {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("%w: expected WorkItem version %d, stored %d",
				ErrApprovalCompletionConflict, req.ExpectedWorkItemVersion, item.ItemVersion)
		}
		items, err := store.ListForInstance(ctx, ex, tenantID, req.InstanceID)
		if err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if prior, dup := stepapproval.DuplicateApprover(req.Continuation, items, item, req.Decision.Approver.PrincipalID); dup {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("%w: %q already decided WorkItem %s of requirement %q",
				stepapproval.ErrDuplicateApprover, req.Decision.Approver.PrincipalID, prior.WorkItemID, item.ApprovalRequirementRef)
		}
		authority, err := req.Authority.Recheck(ctx, ex, CurrentApprovalAuthorityRequest{
			TenantID: tenantID, Item: item, Decision: req.Decision,
			Proposal: req.Start.Proposal, Continuation: req.Continuation, CheckedAt: at,
		})
		if err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("workflow execute: recheck approval authority: %w", err)
		}
		if !authority.Allowed || authority.DecisionRef == "" {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("%w: %s", ErrApprovalAuthorityDenied, authority.Reason)
		}
		if authority.DecisionRef != req.Decision.AuthorityDecisionRef {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("%w: current authority evidence does not match the decision binding", ErrApprovalAuthorityDenied)
		}
		authorityRef = authority.DecisionRef
		ready := item
		if req.Prepare != nil {
			if ready, err = req.Prepare(ctx, ex, item); err != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
			}
		}
		completed, err := stepapproval.Complete(ctx, ex, store, ready, req.Decision, at, req.Meta)
		if err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if req.Record != nil {
			if err := req.Record(ctx, ex, completed); err != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
			}
		}
		vote.completed = completed
	}
	vote.authorityRef = authorityRef

	items, err := store.ListForInstance(ctx, ex, tenantID, req.InstanceID)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	decisions, err := stepapproval.LoadDecisions(ctx, ex, req.Continuation, items)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	resolution, err := stepapproval.Resolve(req.Continuation, items, decisions, values.NewInstant(at),
		stepapproval.Event{Kind: stepapproval.EventDecisionsChanged})
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	vote.resolution = resolution
	if resolution.Outcome == "" {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errCommitWithoutAdvance
	}
	closed, err := cancelOpenSlots(ctx, ex, req.Continuation, items, workitem.TransitionMeta{
		ActorPrincipalID: req.Meta.ActorPrincipalID, Reason: reasonApprovalResolved,
		Detail: "approval node " + item.NodeID + " resolved " + string(resolution.Outcome), EvidenceRef: resolution.Digest, At: at,
	})
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	vote.closed = closed
	return resolution.ToNodeOutcome(item.NodeID), runtime.GovernanceRefs{
		AuthorizationDecisionID: authorityRef,
		DecisionID:              req.Decision.DecisionID,
		HumanTaskID:             item.WorkItemID.String(),
		ProposalRef:             req.Start.Proposal.Revision.MaterialDigest.Digest,
	}, nil
}

// openApprovalNode loads the activation of nodeID the approval addresses: its
// highest recorded attempt.
func openApprovalNode(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID, nodeID string) (runtime.NodeExecution, error) {
	executions, err := (runtime.Store{}).LoadNodeExecutions(ctx, ex, tenantID, instanceID)
	if err != nil {
		return runtime.NodeExecution{}, fmt.Errorf("workflow execute: load node executions for approval: %w", err)
	}
	attempt := highestAttempt(executions, nodeID)
	for _, row := range executions {
		if row.NodeID == nodeID && row.Attempt == attempt {
			return row, nil
		}
	}
	return runtime.NodeExecution{}, fmt.Errorf("%w: approval node %s has no recorded activation", ErrApprovalCompletionConflict, nodeID)
}

// cancelOpenSlots closes every continuation slot still open once its node has
// resolved, so no approver is left holding work the workflow no longer reads.
func cancelOpenSlots(ctx context.Context, ex runtime.Executor, c stepapproval.Continuation, items []workitem.WorkItem, meta workitem.TransitionMeta) ([]workitem.WorkItem, error) {
	open, err := stepapproval.OpenSlots(c, items)
	if err != nil {
		return nil, err
	}
	var closed []workitem.WorkItem
	for _, slot := range open {
		cancelled, err := (workitem.Store{}).Cancel(ctx, ex, slot.TenantID, slot.WorkItemID, slot.ItemVersion, meta)
		if err != nil {
			return nil, err
		}
		closed = append(closed, cancelled)
	}
	return closed, nil
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
	if !servesPinnedInstance(req.Start, published, selection) {
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
	if err := validateApprovalContinuation(req.Start, req.InstanceID, req.Continuation, selection); err != nil {
		return err
	}
	if item.ProposalRef != req.Start.Proposal.Revision.MaterialDigest.Digest {
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

// validateApprovalContinuation checks that the continuation names an APPROVAL
// node of the pinned plan on this instance, bound to the workflow's own
// proposal revision.
func validateApprovalContinuation(start runtime.StartRequest, instanceID uuid.UUID, c stepapproval.Continuation, selection runtime.WorkflowSelection) error {
	if c.WorkflowInstanceID != instanceID {
		return fmt.Errorf("%w: continuation belongs to another instance", ErrApprovalCompletionConflict)
	}
	node, ok := selection.Plan.Node(c.NodeID)
	if !ok || node.Type != workflow.StepApproval {
		return fmt.Errorf("%w: node %s is not APPROVAL in the pinned plan", ErrApprovalCompletionConflict, c.NodeID)
	}
	proposal := start.Proposal.Revision
	if proposal.ProposalRevisionID == "" || proposal.ProposalRevisionID != c.ProposalRevisionID ||
		!sameDigestReference(proposal.MaterialDigest, c.ProposalDigest) {
		return fmt.Errorf("%w: stale or mismatched proposal revision", ErrApprovalCompletionConflict)
	}
	return nil
}

// replayApprovalCompletion answers a vote the durable record already holds on
// a node that has already been advanced. It writes nothing; READY successors a
// crash left undrained are drained.
func (d *Driver) replayApprovalCompletion(ctx context.Context, run runContext, item workitem.WorkItem) (ApprovalCompletionResult, error) {
	tenantID := run.start.TenantID
	var node runtime.NodeExecution
	var inst runtime.Instance
	ready := []string{}
	if err := d.inTenantTx(ctx, tenantID, func(tx runtime.Executor) error {
		var err error
		if node, err = openApprovalNode(ctx, tx, tenantID, run.instanceID, item.NodeID); err != nil {
			return err
		}
		if node.Status != runtime.NodeSucceeded {
			return fmt.Errorf("%w: completed WorkItem has no matching workflow advancement", ErrApprovalCompletionConflict)
		}
		if inst, err = (runtime.Store{}).LoadInstance(ctx, tx, tenantID, run.instanceID); err != nil {
			return err
		}
		executions, err := (runtime.Store{}).LoadNodeExecutions(ctx, tx, tenantID, run.instanceID)
		if err != nil {
			return err
		}
		for _, id := range inst.CurrentNodeIDs {
			for _, row := range executions {
				if row.NodeID == id && row.Attempt == highestAttempt(executions, id) && row.Status == runtime.NodeReady {
					ready = append(ready, id)
				}
			}
		}
		return nil
	}); err != nil {
		return ApprovalCompletionResult{}, err
	}
	base := Result{InstanceVersion: inst.InstanceVersion, Frontier: append([]string(nil), inst.CurrentNodeIDs...)}
	switch {
	case inst.RuntimeStatus.Terminal():
		base.Status = StatusComplete
	case len(ready) > 0:
		var err error
		if base, err = d.drainReady(ctx, run, base, ready); err != nil {
			return ApprovalCompletionResult{}, err
		}
	default:
		base.Status = StatusParked
	}
	return ApprovalCompletionResult{
		Result: base, CompletedItem: item,
		Resolution:   stepapproval.Resolution{Digest: node.OutputArtifactRef},
		AuthorityRef: node.Refs.AuthorizationDecisionID, Replay: true,
	}, nil
}

func (d *Driver) finishApprovalDrain(ctx context.Context, run runContext, result Result, advanced runtime.AdvanceReceipt) (Result, error) {
	if advanced.Complete {
		result.Status = StatusComplete
		return result, nil
	}
	ready, parked := readyAndParked(advanced.Continuations, result.Timers...)
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
