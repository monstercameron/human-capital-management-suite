package execute

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// ResumeRequest presents the immutable typed step resolution completed
// human-work evidence produced, plus the identity of the WorkItem that
// evidence came from. Start is context only: Resume does not create another
// instance. Its resolver and version store re-resolve the exact plan, while
// its proposal, tenant, correlation and subject fields are checked against
// the WorkItem [WorkItemReader] loads before the outcome may enter
// runtime.Advance.
//
// WorkItemID and ExpectedItemVersion name the durable WorkItem, never carry
// it (WF-RUN-028): Resume loads the row itself, inside the same transaction
// as the advancement it feeds, and refuses [ErrWorkItemDrift] the instant the
// stored row disagrees with what this request or the pinned plan expects.
//
// Outcome.OutputDigest is the digest of the typed step resolution, not
// necessarily the stored WorkItem's own completed-output digest. For an
// approval quorum, for example, the former binds the aggregate resolution
// while the latter binds one approver's decision.
type ResumeRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	WorkItemID              uuid.UUID
	ExpectedWorkItemVersion int64
	Outcome                 frontier.NodeOutcome
	Refs                    runtime.GovernanceRefs
	RecordedAt              time.Time
}

// Resume advances a WAITING APPROVAL or TASK from the durable WorkItem
// [WorkItemReader] loads for req.WorkItemID, then drains any ordinary READY
// successors exactly as Execute does. It never polls and it does not
// complete the WorkItem itself.
func (d *Driver) Resume(ctx context.Context, req ResumeRequest) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.resume", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	selection, err := validateResumeConfig(ctx, req, d.opts.Items)
	if err != nil {
		return Result{}, err
	}
	run := runContext{
		start: req.Start, selection: selection, instanceID: req.InstanceID,
		traceID: d.opts.Instrumentation.TraceID(ctx),
	}
	at := req.RecordedAt.UTC()
	if req.RecordedAt.IsZero() {
		at = d.opts.Clock().UTC()
	}
	ctx, release, err := d.acquireInstanceLease(ctx, req.Start.TenantID, req.InstanceID)
	if err != nil {
		return Result{}, err
	}
	defer func() { retErr = releasing(retErr, release) }()

	// OBS-024: a Resume that just advanced from a completed work item is
	// either an APPROVAL_COMPLETED or a TASK_SUBMITTED event, decided from
	// the pinned plan's own node type for the node the advancement names —
	// never from a caller-asserted kind. The entry is recorded by advanceOnce
	// on the advance transaction itself, so it commits beside the outcome.
	advanced, created, evidenceIDs, timers, err := d.advanceOnce(ctx, run, req.ExpectedInstanceVersion, at, 1,
		func(ctx context.Context, ex runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error) {
			item, loadErr := d.opts.Items.Load(ctx, ex, req.Start.TenantID, req.WorkItemID)
			if loadErr != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil, loadErr
			}
			// Human-work items carry no stored causal identity (OBS-013
			// persists it on timer, job, outbox and signal envelopes, not
			// on work items), so this path always advances unlinked.
			outcome, refs, driftErr := checkWorkItemDrift(req, selection, item)
			if driftErr != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil, driftErr
			}
			// REV-008-01: a served TASK node resumes through the certified
			// task contract, not the drift check alone.
			if node, ok := selection.Plan.Node(item.NodeID); ok && node.Type == workflow.StepTask && d.opts.Tasks != nil {
				outcome, refs, driftErr = d.checkTaskResume(ctx, ex, req, selection, node, item, outcome, refs, values.NewInstant(at))
				if driftErr != nil {
					return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil, driftErr
				}
			}
			return outcome, refs, nil, nil
		},
		func(advanced runtime.AdvanceReceipt) (string, string, bool) {
			node, ok := selection.Plan.Node(advanced.NodeID)
			if !ok {
				return "", "", false
			}
			switch node.Type {
			case workflow.StepApproval:
				return EvidenceKindApprovalCompleted, req.WorkItemID.String(), true
			case workflow.StepTask:
				return EvidenceKindTaskSubmitted, req.WorkItemID.String(), true
			}
			return "", "", false
		})
	if err != nil {
		settled := d.settlePause(ctx, run, at, err)
		if paused, ok := pausedResult(settled, Result{}); ok {
			return paused, nil
		}
		return Result{}, settled
	}

	result := Result{
		Advances:        []runtime.AdvanceReceipt{advanced},
		WorkItems:       created,
		Timers:          timers,
		InstanceVersion: advanced.NewInstanceVersion,
		Frontier:        append([]string(nil), advanced.Frontier...),
		EvidenceIDs:     evidenceIDs,
	}
	return d.continueAfterAdvance(ctx, run, result, advanced)
}

// validateResumeConfig checks everything Resume can check before it ever
// opens a transaction: wiring, request shape and the pinned plan's identity.
// It never touches the WorkItem -- that load happens inside the advancement
// transaction, in [checkWorkItemDrift], per WF-RUN-028.
func validateResumeConfig(ctx context.Context, req ResumeRequest, items WorkItemReader) (runtime.WorkflowSelection, error) {
	if items == nil {
		return runtime.WorkflowSelection{}, invalid("resume has no WorkItemReader")
	}
	if req.Start.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.WorkItemID == uuid.Nil ||
		req.ExpectedInstanceVersion < 1 || req.ExpectedWorkItemVersion < 1 {
		return runtime.WorkflowSelection{}, invalid(
			"resume requires tenant, instance, work item and positive expected instance/work-item versions")
	}
	return resolvePinnedPlan(ctx, req.Start, "resume")
}

// resolvePinnedPlan resolves the exact published version a resume must
// advance against -- the ACTIVE one, or the superseded one the instance
// pinned ([servesPinnedInstance]) -- and refuses anything that is not it. It is shared by
// [Driver.Resume] and [Driver.ResumeTimer] so that "which plan may a parked
// instance advance on" is answered in one place rather than two.
func resolvePinnedPlan(ctx context.Context, start runtime.StartRequest, what string) (runtime.WorkflowSelection, error) {
	if start.Resolver == nil {
		return runtime.WorkflowSelection{}, invalid("%s has no WorkflowResolver", what)
	}
	if start.Versions == nil {
		return runtime.WorkflowSelection{}, invalid("%s has no exact version Store", what)
	}
	selection, err := start.Resolver.ResolveWorkflow(ctx, start)
	if err != nil {
		return runtime.WorkflowSelection{}, fmt.Errorf("workflow execute: resolve %s workflow: %w", what, err)
	}
	if selection.Plan == nil || selection.WorkflowID == "" {
		return runtime.WorkflowSelection{}, invalid("WorkflowResolver returned no workflow id or plan for %s", what)
	}
	published, err := version.Resolve(start.Versions, selection.WorkflowID, selection.Pin)
	if err != nil {
		return runtime.WorkflowSelection{}, fmt.Errorf("workflow execute: resolve %s version: %w", what, err)
	}
	if !servesPinnedInstance(start, published, selection) {
		return runtime.WorkflowSelection{}, invalid("%s plan is not the exact active published version", what)
	}
	return selection, nil
}

// servesPinnedInstance reports whether a live instance may keep advancing on
// published: the version is ACTIVE and is the resolver's plan, or it is the
// exact version the instance pinned ([runtime.StartRequest.PinnedCompiledPlanDigest])
// and is QUARANTINED only because a later activation superseded it. A
// governed quarantine is not a supersession and is refused here; its
// live-instance disposition (PAUSE, BLOCK, CONTINUE) is enforced in the
// advance transaction by applyQuarantine.
func servesPinnedInstance(start runtime.StartRequest, published version.CompiledVersion, selection runtime.WorkflowSelection) bool {
	if published.CompiledPlanDigest != selection.Plan.Digest() {
		return false
	}
	if published.Status == version.StatusActive {
		return true
	}
	return start.PinnedCompiledPlanDigest != "" && start.PinnedCompiledPlanDigest == published.CompiledPlanDigest &&
		published.QuarantinedBySupersession()
}

// checkWorkItemDrift compares the durable row [WorkItemReader] loaded --
// never a struct the caller assembled -- against req and the pinned plan,
// and refuses [ErrWorkItemDrift] on any difference: status, item version,
// completion evidence (via item.Validate, which a COMPLETED row can only
// pass with a completer, a completed-at instant and a well-formed output
// digest all present) and instance/node binding. The attempt is always 1 in
// this bounded, single-attempt driver, so there is no separate attempt field
// to compare.
//
// The NodeOutcome fed to the frontier is derived from the stored row: NodeID
// always comes from item.NodeID (never trusted from req.Outcome alone), and
// [GovernanceRefs.HumanTaskID] is always item.WorkItemID.
func checkWorkItemDrift(
	req ResumeRequest, selection runtime.WorkflowSelection, item workitem.WorkItem,
) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if err := item.Validate(); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift("stored work item %s fails its own invariants: %v", item.WorkItemID, err)
	}
	if !closedItemCarries(item.Status, req.Outcome.Outcome) {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"work item %s is %s, which cannot carry outcome %q", item.WorkItemID, item.Status, req.Outcome.Outcome)
	}
	if item.ItemVersion != req.ExpectedWorkItemVersion {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"work item %s expected version %d, stored %d", item.WorkItemID, req.ExpectedWorkItemVersion, item.ItemVersion)
	}
	if item.TenantID != req.Start.TenantID || item.WorkflowInstanceID != req.InstanceID {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"work item %s is bound to another tenant or instance", item.WorkItemID)
	}
	if item.CorrelationID != req.Start.CorrelationID || item.ProposalRef != req.Start.Proposal.Revision.MaterialDigest.Digest {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"work item %s is bound to another correlation or proposal", item.WorkItemID)
	}
	if !sameStrings(item.SubjectRefs, req.Start.BusinessSubjectRefs) {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"work item %s subject binding differs from the workflow", item.WorkItemID)
	}
	node, ok := selection.Plan.Node(item.NodeID)
	if !ok || (node.Type != workflow.StepApproval && node.Type != workflow.StepTask) {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"work item %s names node %s, which is not an APPROVAL or TASK in the pinned plan", item.WorkItemID, item.NodeID)
	}

	outcome := req.Outcome
	if outcome.NodeID == "" {
		outcome.NodeID = item.NodeID
	}
	if outcome.NodeID != item.NodeID || outcome.Await != frontier.AwaitNone || outcome.Failed || outcome.Outcome == "" || outcome.OutputDigest == "" {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid(
			"resume requires a completed typed outcome for work item node %s", item.NodeID)
	}
	refs := req.Refs
	if refs.HumanTaskID != "" && refs.HumanTaskID != item.WorkItemID.String() {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("typed outcome names another human task")
	}
	refs.HumanTaskID = item.WorkItemID.String()
	return outcome, refs, nil
}

// closedItemCarries reports whether a WorkItem in status may be the evidence
// for outcome. A COMPLETED item carries whatever its resolution names. An item
// closed without a completion carries only the routes that closure means
// (WF-STEP-003): EXPIRED for an expired item; INVALIDATED (an authority
// recheck closed it) or CANCELLED for a cancelled one. Any other status is
// still open and carries nothing.
func closedItemCarries(status workitem.Status, outcome workflow.Outcome) bool {
	switch status {
	case workitem.StatusCompleted:
		return true
	case workitem.StatusExpired:
		return outcome == workflow.Outcome("EXPIRED")
	case workitem.StatusCancelled:
		return outcome == workflow.Outcome("INVALIDATED") || outcome == workflow.Outcome("CANCELLED")
	default:
		return false
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func readyAndParked(records []runtime.ContinuationRecord, timers ...TimerHandle) ([]string, bool) {
	ready := []string{}
	parked := false
	for _, rec := range records {
		switch rec.Kind {
		case frontier.IntentReady:
			if retryParked(timers, rec.TargetNodeID) {
				parked = true
				continue
			}
			ready = append(ready, rec.TargetNodeID)
		case frontier.IntentWorkItemRequired, frontier.IntentTimerRequired, frontier.IntentSignalSubscriptionRequired:
			// A durable timer or signal subscription parks the instance
			// exactly as human work does: the driver has nothing left to run,
			// and something outside it -- a caller with its own clock
			// reading, a matched signal, or a person -- decides when the
			// instance moves again.
			parked = true
		}
	}
	sort.Strings(ready)
	return ready, parked
}
