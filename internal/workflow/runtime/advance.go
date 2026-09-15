package runtime

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Additional stable refusal codes this file introduces for WF-RUN-025, on top
// of the WF-RUN-001 codes in errors.go and the WF-RUN-023 codes in start.go.
// A refusal that happens while [frontier.Advance] itself is walking the graph
// surfaces as a [frontier.Error] instead -- check it with [frontier.CodeOf].
// Every refusal this file raises directly uses this package's own [Error];
// check it with [CodeOf].
const (
	// CodeAdvancePlanMismatch reports an Advance call pinned to a plan whose
	// digest disagrees with the instance's own compiled_plan_hash.
	CodeAdvancePlanMismatch = "ADVANCE_PLAN_MISMATCH"
	// CodeJoinsNotDurable reports a plan that declares a JOIN node. Durable
	// Advance does not reconstruct join arrival counters across calls in this
	// phase -- PARALLEL/JOIN remain gated behind P1B evidence
	// (planning/specs/workflow-runtime.md), and no plan this phase compiles
	// declares one; [frontier.Advance] itself has supported JOIN since
	// WF-RUN-024; only this package's durable reconstruction does not yet.
	CodeJoinsNotDurable = "JOINS_NOT_DURABLE"
)

// AdvanceRequest is one caller-driven advancement of a single node execution
// attempt, fenced by the instance row's own optimistic version.
type AdvanceRequest struct {
	TenantID                uuid.UUID
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	// Attempt is the attempt number of the node execution this outcome
	// concludes. It must match the attempt [ContinuationSink.MarkReady] (or
	// [Start], for the start node) most recently recorded for this node.
	Attempt int

	// Plan is the exact compiled plan this instance is pinned to. Advance
	// refuses when its digest disagrees with the instance's own
	// compiled_plan_hash: an instance advances against the plan it started
	// on, never a migration in disguise.
	Plan *workflow.CompiledWorkflow

	// Outcome is the typed result the caller's step handler produced. Its
	// NodeID names which node this call advances.
	Outcome frontier.NodeOutcome

	Refs    GovernanceRefs
	TraceID string
	// ExecutionContextDigest, when set, is the digest of the execution
	// context the caller delivered to the step (WF-RUN-040). Advance refuses
	// with [CodeContextDrift] when the instance pinned a different one, so a
	// step never commits under a context the instance did not start with.
	ExecutionContextDigest string
	// Causal is optional metadata copied to derived durable continuations.
	// It never participates in replay identity or authorization.
	Causal *CausalMetadata

	// Revalidation is optional for legacy/non-material nodes. When present it
	// is evaluated before Advance reads runtime state, and a changed fact is a
	// typed refusal that cannot reach the node write path.
	Revalidation *PromotionRevalidation

	// RecordedAt is the instant this call is happening. This package never
	// reads a wall clock: it stamps the instance's started_at on the first
	// advancement, the completed_at of a finished node execution, and the
	// instance's own completed_at when the outcome completes the instance.
	RecordedAt time.Time

	// Sink persists every continuation record this advancement derives, one
	// call per [frontier.Intent] in the resulting transition, dispatched by
	// [dispatchContinuation]. It is called only on a fresh (non-replay)
	// advancement: WF-RUN-025's "persists ... exactly once" is enforced by
	// never calling it a second time for an advancement this package
	// recognizes as already applied, not by the sink deduplicating on its own.
	Sink ContinuationSink
}

func (r AdvanceRequest) validate() error {
	instance := r.InstanceID.String()
	switch {
	case r.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, instance, "", "tenant id must not be the nil UUID")
	case r.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, instance, "", "instance id must not be the nil UUID")
	case r.ExpectedInstanceVersion < 1:
		return refuse(CodeInvalidRecord, instance, "", "expected instance version must be at least 1")
	case r.Attempt < 1:
		return refuse(CodeInvalidRecord, instance, "", "attempt must be at least 1")
	case r.Plan == nil:
		return refuse(CodeInvalidRecord, instance, "", "no compiled plan supplied")
	case r.Outcome.NodeID == "":
		return refuse(CodeInvalidRecord, instance, "", "outcome names no node id")
	case r.Sink == nil:
		return refuse(CodeInvalidRecord, instance, r.Outcome.NodeID, "no continuation sink supplied")
	case r.RecordedAt.IsZero():
		return refuse(CodeInvalidRecord, instance, r.Outcome.NodeID,
			"recorded_at must be supplied; this package never reads a wall clock")
	}
	return nil
}

// AdvanceReceipt is everything one [Advance] call produced.
type AdvanceReceipt struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Attempt    int

	// CompletedState is the completed node's resulting state, spelled as
	// [frontier.NodeState] (READY, RUNNING, WAITING, SUCCEEDED, FAILED,
	// RETRYING, SKIPPED, OVERRIDDEN, COMPENSATED or CANCELLED).
	CompletedState string
	RouteKey       string
	OutputDigest   string

	NewInstanceVersion int64
	Frontier           []string

	Complete     bool
	TerminalCode string

	// Continuations is every continuation record this advancement derived and
	// persisted through the [ContinuationSink]. On a replayed (already
	// applied) advancement this is always empty: the records were persisted
	// exactly once, by the original call, and this package does not requery
	// a caller-supplied sink to relist them -- [ContinuationSink] declares no
	// read method, on purpose, so a sink can be a plain append-only write
	// target. A caller that needs to know what was scheduled reads its own
	// sink-backed store directly.
	Continuations []ContinuationRecord
	// Replay reports that this receipt was reconstructed from an
	// already-applied advancement (same expected instance version and outcome
	// digest resubmitted) rather than freshly computed.
	Replay bool

	digest string
}

// Digest is the receipt's content identity.
func (r AdvanceReceipt) Digest() string { return r.digest }

// Advance applies one caller-driven advancement in a single fenced
// transaction: it records the node attempt result and typed output digest,
// applies [frontier.Advance], writes the new node executions, instance
// version and frontier, and persists every derived continuation record
// exactly once through req.Sink.
//
// tx is the transaction the caller began and will commit or roll back: every
// statement Advance issues goes through it, so a failure at any point --
// including a [ContinuationSink] method returning an error -- leaves the
// whole advancement uncommitted when the caller rolls back, never half of it.
//
// A stale ExpectedInstanceVersion commits nothing. An identical retry of the
// complete request returns its durable receipt without reapplying anything,
// but only while the instance remains at that receipt's resulting version.
// Once a later advancement moves the instance again, the old request is stale.
func Advance(ctx context.Context, tx Executor, req AdvanceRequest) (ret0 AdvanceReceipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.advance", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return AdvanceReceipt{}, err
	}
	if req.Revalidation != nil {
		if _, err := EvaluatePromotionRevalidation(*req.Revalidation); err != nil {
			return AdvanceReceipt{}, err
		}
	}
	store := Store{}

	inst, err := store.LoadInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return AdvanceReceipt{}, err
	}
	if inst.CompiledPlanHash != req.Plan.Digest() {
		return AdvanceReceipt{}, refuse(CodeAdvancePlanMismatch, req.InstanceID.String(), req.Outcome.NodeID,
			"instance pins compiled plan %s; Advance was called with a plan digesting to %s",
			inst.CompiledPlanHash, req.Plan.Digest())
	}
	if node, ok := req.Plan.Node(req.Outcome.NodeID); ok && !NodeAllowsMode(node, inst.ExecutionMode) {
		// WF-RUN-040: the durable instance mode, not the caller, decides.
		return AdvanceReceipt{}, refuse(CodeModeNotAllowed, req.InstanceID.String(), req.Outcome.NodeID,
			"node admits modes %v; the instance runs in %s", node.AllowedModes, inst.ExecutionMode)
	}
	if req.ExecutionContextDigest != "" && inst.EffectiveContextRef != "" && req.ExecutionContextDigest != inst.EffectiveContextRef {
		return AdvanceReceipt{}, refuse(CodeContextDrift, req.InstanceID.String(), req.Outcome.NodeID,
			"step ran under execution context %s; the instance pinned %s", req.ExecutionContextDigest, inst.EffectiveContextRef)
	}

	requestDigest := computeAdvanceRequestDigest(req)
	stored, found, err := loadAdvancementReceipt(ctx, tx, req)
	if err != nil {
		return AdvanceReceipt{}, err
	}
	if found {
		if stored.RequestDigest == requestDigest && inst.InstanceVersion == stored.ResultingInstanceVersion {
			return stored.Receipt, nil
		}
		return AdvanceReceipt{}, staleError(req.InstanceID, req.Outcome.NodeID, req.ExpectedInstanceVersion, inst.InstanceVersion)
	}
	if inst.InstanceVersion != req.ExpectedInstanceVersion {
		return AdvanceReceipt{}, staleError(req.InstanceID, req.Outcome.NodeID, req.ExpectedInstanceVersion, inst.InstanceVersion)
	}
	// WF-RUN-008's pause gate sits after the replay branch on purpose: a
	// byte-identical retry of an advancement that already committed is a
	// read of durable evidence, and a pause requested afterwards does not
	// retroactively unmake it.
	if err := checkPauseGate(ctx, tx, inst, req.Plan); err != nil {
		return AdvanceReceipt{}, err
	}

	state, err := loadFrontierState(ctx, tx, store, inst, req.Plan)
	if err != nil {
		return AdvanceReceipt{}, err
	}
	completingBefore, hasBefore := state.Node(req.Outcome.NodeID)
	if !hasBefore {
		return AdvanceReceipt{}, refuse(CodeNodeExecutionNotFound, req.InstanceID.String(), req.Outcome.NodeID,
			"no recorded attempt for this node; Advance requires a node MarkReady or Start already recorded")
	}

	tr, err := frontier.Advance(req.Plan, state, req.Outcome)
	if err != nil {
		return AdvanceReceipt{}, err
	}

	final := NodeTransition{
		TenantID: req.TenantID, InstanceID: req.InstanceID, NodeID: req.Outcome.NodeID, Attempt: req.Attempt,
		OutputArtifactRef: tr.OutputDigest,
		Refs:              req.Refs,
		ErrorClass:        req.Outcome.ErrorClass,
		TraceID:           req.TraceID,
		CompletedAt:       completedAtFor(tr.CompletedState, req.RecordedAt),
	}
	nextVersion, err := applyNodeStatusPath(ctx, tx, store, req.TenantID, req.InstanceID, req.Outcome.NodeID, req.Attempt,
		NodeStatus(completingBefore.State), NodeStatus(tr.CompletedState), req.ExpectedInstanceVersion, final)
	if err != nil {
		return AdvanceReceipt{}, err
	}

	// A successor the instance has already executed (a re-approval routing
	// back to an approval gate, a revalidation loop) is a new attempt of that
	// node, never a second attempt 1: the (instance, node, attempt) identity is
	// the primary key and the driver picks the highest attempt as current.
	existing, err := store.LoadNodeExecutions(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return AdvanceReceipt{}, err
	}
	// WF-RUN-038: every node execution this advancement inserts records its
	// fingerprint from the plan (digest-checked against the pin above) and the
	// runtime version the instance's execution context pinned.
	fingerprints, err := pinnedFingerprinter(ctx, tx, req.TenantID, req.InstanceID, req.Plan, req.RecordedAt)
	if err != nil {
		return AdvanceReceipt{}, err
	}
	activated := map[string]int{}
	for _, succ := range tr.Successors {
		succNode, ok := req.Plan.Node(succ.NodeID)
		if !ok {
			return AdvanceReceipt{}, refuse(CodeInvalidRecord, req.InstanceID.String(), succ.NodeID,
				"successor names a node the plan does not declare")
		}
		activated[succ.NodeID] = nextAttemptFor(existing, succ.NodeID)
		ne := NewNodeExecution(req.TenantID, req.InstanceID, succ.NodeID, activated[succ.NodeID], succNode.Type, NodeStatus(succ.State))
		_, bumped, err := store.RecordNodeExecution(ctx, tx, ne, nextVersion)
		if err != nil {
			return AdvanceReceipt{}, err
		}
		if err := fingerprints.record(ctx, tx, ne); err != nil {
			return AdvanceReceipt{}, err
		}
		nextVersion = bumped
	}
	for _, skipped := range tr.Skipped {
		skipNode, ok := req.Plan.Node(skipped)
		if !ok {
			continue
		}
		ne := NewNodeExecution(req.TenantID, req.InstanceID, skipped, nextAttemptFor(existing, skipped), skipNode.Type, NodeSkipped)
		_, bumped, err := store.RecordNodeExecution(ctx, tx, ne, nextVersion)
		if err != nil {
			return AdvanceReceipt{}, err
		}
		if err := fingerprints.record(ctx, tx, ne); err != nil {
			return AdvanceReceipt{}, err
		}
		nextVersion = bumped
	}

	// A standing pause request survives the advancement it did not get to
	// stop: writing RUNNING here would drop it, and the next boundary would
	// have nothing to apply (WF-RUN-008).
	newStatus := InstanceRunning
	if inst.RuntimeStatus == InstancePauseRequested {
		newStatus = InstancePauseRequested
	}
	var completedAt *time.Time
	dims := Dimensions{}
	terminalCode := ""
	if tr.Complete {
		newStatus = InstanceStatus(tr.Terminal.RuntimeStatus)
		at := req.RecordedAt
		completedAt = &at
		dims = Dimensions{
			RequestState:     tr.Terminal.Lifecycle.RequestState,
			ExecutionState:   tr.Terminal.Lifecycle.ExecutionState,
			BusinessState:    tr.Terminal.Lifecycle.BusinessState,
			ConsistencyState: tr.Terminal.Lifecycle.ConsistencyState,
			ObligationState:  tr.Terminal.Lifecycle.ObligationState,
		}
		terminalCode = tr.Terminal.TerminalCode
	} else if !inst.CompletionDimensions.Empty() {
		dims = inst.CompletionDimensions
	}
	var startedAt *time.Time
	if inst.RuntimeStatus == InstanceCreated {
		at := req.RecordedAt
		startedAt = &at
	}
	// WF-STEP-003: a plan terminal declaring CANCELLED (an APPROVAL's EXPIRED
	// or CANCELLED route) ends a live instance, which the instance state
	// machine only allows through CANCELLING. The routed terminal takes that
	// hop in the same transaction rather than being refused as illegal.
	if hop, needed := cancellingHop(inst.RuntimeStatus, newStatus); needed {
		stepped, hopErr := store.RecordInstanceState(ctx, tx, InstanceTransition{
			TenantID: req.TenantID, InstanceID: req.InstanceID, ExpectedVersion: nextVersion,
			Status: hop, CurrentNodeIDs: inst.CurrentNodeIDs, VariableRevisionHead: inst.VariableRevisionHead,
			EffectiveContextRef: inst.EffectiveContextRef, LastCheckpointRef: inst.LastCheckpointRef,
			CompletionDimensions: inst.CompletionDimensions, StartedAt: startedAt,
		})
		if hopErr != nil {
			return AdvanceReceipt{}, hopErr
		}
		nextVersion = stepped.InstanceVersion
	}

	updated, err := store.RecordInstanceState(ctx, tx, InstanceTransition{
		TenantID:             req.TenantID,
		InstanceID:           req.InstanceID,
		ExpectedVersion:      nextVersion,
		Status:               newStatus,
		CurrentNodeIDs:       tr.Frontier,
		VariableRevisionHead: inst.VariableRevisionHead,
		EffectiveContextRef:  inst.EffectiveContextRef,
		LastCheckpointRef:    inst.LastCheckpointRef,
		CompletionDimensions: dims,
		StartedAt:            startedAt,
		CompletedAt:          completedAt,
	})
	if err != nil {
		return AdvanceReceipt{}, err
	}

	conts := make([]ContinuationRecord, 0, len(tr.Intents))
	for _, in := range tr.Intents {
		rec := ContinuationRecord{
			TenantID: req.TenantID, InstanceID: req.InstanceID,
			SourceNodeID: req.Outcome.NodeID, SourceAttempt: req.Attempt,
			TargetNodeID: in.NodeID, Kind: in.Kind, RouteKey: in.RouteKey, Ref: in.Ref, TerminalCode: in.TerminalCode,
			TargetAttempt: activated[in.NodeID],
			RecordedAt:    req.RecordedAt, Causal: normalizeCausal(req.Causal),
		}
		if dispatchErr := dispatchContinuation(ctx, tx, req.Sink, rec); dispatchErr != nil {
			return AdvanceReceipt{}, wrap(CodeStorageFailed, req.InstanceID.String(), in.NodeID, dispatchErr,
				"persist continuation record for intent %s", in.Kind)
		}
		conts = append(conts, rec)
	}

	receipt := AdvanceReceipt{
		TenantID:           req.TenantID,
		InstanceID:         req.InstanceID,
		NodeID:             req.Outcome.NodeID,
		Attempt:            req.Attempt,
		CompletedState:     string(tr.CompletedState),
		RouteKey:           tr.RouteKey,
		OutputDigest:       tr.OutputDigest,
		NewInstanceVersion: updated.InstanceVersion,
		Frontier:           append([]string(nil), updated.CurrentNodeIDs...),
		Complete:           tr.Complete,
		TerminalCode:       terminalCode,
		Continuations:      conts,
	}
	receipt.digest = computeAdvanceReceiptDigest(receipt)
	if err := recordAdvancementReceipt(ctx, tx, req, requestDigest, receipt); err != nil {
		return AdvanceReceipt{}, err
	}
	return receipt, nil
}

// completedAtFor stamps a completion instant only for a status that actually
// finishes the attempt, matching [NodeTransition.Validate]'s own rule.
func completedAtFor(state frontier.NodeState, at time.Time) *time.Time {
	if NodeStatus(state).Finished() {
		t := at
		return &t
	}
	return nil
}

// nodeTransitionPath returns the sequence of legal hops [LegalNodeTransition]
// requires to reach target from current.
//
// [frontier.Advance] reasons about a node's lifecycle as READY (or WAITING)
// producing a terminal outcome in one step -- the caller already ran it, and
// reports what happened. The durable node state machine WF-RUN-001 shipped
// (state.go, unedited by this ticket) requires the intermediate RUNNING state
// the spec's own diagram names before SUCCEEDED, WAITING, FAILED or
// COMPENSATED, and a further FAILED before RETRYING. Rather than editing that
// state machine -- out of scope for this ticket, and arguably correct on its
// own terms -- Advance walks it explicitly: recording RUNNING (and, for a
// retry, FAILED) before the outcome's own resulting state is not a workaround
// so much as making the omitted middle explicit, since the caller's step
// handler did in fact run the node before reporting this outcome.
func nodeTransitionPath(current, target NodeStatus) []NodeStatus {
	if LegalNodeTransition(current, target) {
		return []NodeStatus{target}
	}
	if LegalNodeTransition(current, NodeRunning) {
		if LegalNodeTransition(NodeRunning, target) {
			return []NodeStatus{NodeRunning, target}
		}
		if target == NodeRetrying && LegalNodeTransition(NodeRunning, NodeFailed) && LegalNodeTransition(NodeFailed, NodeRetrying) {
			return []NodeStatus{NodeRunning, NodeFailed, NodeRetrying}
		}
	}
	// No legal path found; return the direct hop so the underlying store
	// refusal names exactly the transition that failed, rather than this
	// function inventing a different error.
	return []NodeStatus{target}
}

// applyNodeStatusPath records every hop [nodeTransitionPath] names, applying
// the caller's evidence (output ref, governance refs, error class, trace,
// completion instant) only on the final hop: an intermediate RUNNING or
// FAILED stamp carries no output of its own.
func applyNodeStatusPath(
	ctx context.Context, ex Executor, store Store,
	tenantID, instanceID uuid.UUID, nodeID string, attempt int,
	current, target NodeStatus, expectedVersion int64, final NodeTransition,
) (int64, error) {
	path := nodeTransitionPath(current, target)
	version := expectedVersion
	for i, step := range path {
		t := NodeTransition{
			TenantID: tenantID, InstanceID: instanceID, NodeID: nodeID, Attempt: attempt,
			ExpectedInstanceVersion: version,
			Status:                  step,
		}
		if i == len(path)-1 {
			t.OutputArtifactRef = final.OutputArtifactRef
			t.Refs = final.Refs
			t.ErrorClass = final.ErrorClass
			t.TraceID = final.TraceID
			t.CompletedAt = final.CompletedAt
		}
		_, bumped, err := store.RecordNodeTransition(ctx, ex, t)
		if err != nil {
			return 0, err
		}
		version = bumped
	}
	return version, nil
}

// loadFrontierState reconstructs a [frontier.InstanceState] snapshot from
// durable rows: the instance's own frontier and every node execution's latest
// attempt. It refuses a plan that declares a JOIN ([CodeJoinsNotDurable]):
// join arrival counters are not among the columns migrations/00016 or
// migrations/00018 carry, and no plan this phase compiles declares one.
func loadFrontierState(ctx context.Context, ex Executor, store Store, inst Instance, plan *workflow.CompiledWorkflow) (frontier.InstanceState, error) {
	for _, n := range plan.Nodes {
		if n.Type == workflow.StepJoin {
			return frontier.InstanceState{}, refuse(CodeJoinsNotDurable, inst.InstanceID.String(), n.ID,
				"plan declares a JOIN node; durable Advance does not reconstruct join arrival counters across calls")
		}
	}

	rows, err := store.LoadNodeExecutions(ctx, ex, inst.TenantID, inst.InstanceID)
	if err != nil {
		return frontier.InstanceState{}, err
	}
	latest := make(map[string]NodeExecution, len(rows))
	for _, r := range rows {
		if cur, ok := latest[r.NodeID]; !ok || r.Attempt > cur.Attempt {
			latest[r.NodeID] = r
		}
	}
	nodeIDs := make([]string, 0, len(latest))
	for id := range latest {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	state := frontier.InstanceState{
		InstanceID: inst.InstanceID.String(),
		WorkflowID: inst.WorkflowID,
		Version:    inst.WorkflowVersion,
		PlanDigest: plan.Digest(),
		Sequence:   uint64(len(rows)),
		Completed:  inst.RuntimeStatus.Terminal(),
	}
	for _, id := range nodeIDs {
		r := latest[id]
		state.Nodes = append(state.Nodes, frontier.NodeStatus{
			NodeID:       id,
			State:        frontier.NodeState(r.Status),
			OutputDigest: r.OutputArtifactRef,
			Attempts:     uint32(r.Attempt - 1),
		})
	}
	frontierIDs := make([]string, 0, len(state.Nodes))
	for _, n := range state.Nodes {
		if n.State.Active() {
			frontierIDs = append(frontierIDs, n.NodeID)
		}
	}
	sort.Strings(frontierIDs)
	state.Frontier = frontierIDs

	// This deliberately does not cross-check state.Frontier against
	// inst.CurrentNodeIDs. inst was read by an earlier statement in this same
	// transaction, and under READ COMMITTED a later statement (the
	// LoadNodeExecutions call above) can legitimately observe a concurrent
	// commit the earlier one did not yet see -- that disagreement is a
	// concurrent writer getting here first, not corruption. The version
	// fence, not this reconstruction, is what has to catch that: either
	// frontier.Advance itself refuses cleanly below (the completing node's
	// reconstructed status is no longer active, CodeNodeNotActive) or the
	// write path's own compare-and-set refuses with CodeStaleInstance against
	// the database's actual current version when it runs. An assertion here
	// comparing two different point-in-time reads would only add a third,
	// less precise refusal for a case those two already cover correctly, and
	// under real concurrency (WF-RUN-025's own Race case) it fires on a
	// perfectly safe outcome.
	return state, nil
}

// nextAttemptFor returns the attempt number a fresh activation of nodeID
// must use: one more than the highest attempt already recorded for it on the
// instance, or 1 when the node has never been activated.
func nextAttemptFor(existing []NodeExecution, nodeID string) int {
	highest := 0
	for _, row := range existing {
		if row.NodeID == nodeID && row.Attempt > highest {
			highest = row.Attempt
		}
	}
	return highest + 1
}
