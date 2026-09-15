package inspect

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// LoadRequest names one instance to inspect from durable storage and the
// already-evaluated decisions to render it under.
type LoadRequest struct {
	TenantID      uuid.UUID
	InstanceID    uuid.UUID
	Authorization Authorization
	WorkItems     WorkItemAuthorization
	// Versions is the durable compiled-version registry the instance's pin
	// is resolved against (internal/data/workflowversionstore.Store in every
	// real composition). It is bound to the read's transaction through
	// [version.BindTx]. Nil renders the version record UNAVAILABLE and names
	// the gap: a pin the inspector could not resolve is not a resolved pin.
	Versions version.Store
}

// Load reads one instance's durable execution record and renders it: the
// instance row and its pinned compiled version, node executions with their
// attempts and retry timers, every durable timer, work items and their
// transitions, the pinned execution context and its digest, advancement
// receipts, the instance lease and latest checkpoint, the outbox rows and
// reconciliation jobs behind each recorded effect reference, and the trace
// ids that correlate them. Families with no durable store in this repository
// are listed as UNAVAILABLE rather than invented.
//
// ex must be a transaction already scoped to TenantID
// (internal/data/tenancy.WithTenant); every table read is row-level-security
// protected, and every read also filters by TenantID. Load writes nothing.
//
// A caller who may not learn the instance exists, an instance that does not
// exist and another tenant's instance all return [ErrNotDisclosable], and the
// latter two are byte-identical so the answer is not an existence oracle. A
// non-disclosable decision is honored before any table is read.
func Load(ctx context.Context, ex dbport.Conn, req LoadRequest) (ret0 DurableView, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.inspect.load", observe.Attrs{
		observe.KeyTenant: req.TenantID.String(), observe.KeyInstance: req.InstanceID.String(),
	})
	defer func() { observe.DoneWith(obsOp, retErr, ret0.Completeness.Complete) }()

	auth := req.Authorization
	if err := auth.Validate(); err != nil {
		return DurableView{}, err
	}
	if !auth.InstanceDisclosable {
		return DurableView{}, fmt.Errorf("%w: %s", ErrNotDisclosable, auth.DenialReason)
	}
	if ex == nil || req.TenantID == uuid.Nil || req.InstanceID == uuid.Nil {
		return DurableView{}, fmt.Errorf("%w: a load needs an executor, a tenant and an instance", ErrInvalidRequest)
	}

	inst, err := (runtime.Store{}).LoadInstance(ctx, ex, req.TenantID, req.InstanceID)
	if err != nil {
		if runtime.CodeOf(err) == runtime.CodeInstanceNotFound {
			return DurableView{}, ErrNotDisclosable
		}
		return DurableView{}, fmt.Errorf("inspect: load instance: %w", err)
	}
	nodes, err := (runtime.Store{}).LoadNodeExecutions(ctx, ex, req.TenantID, req.InstanceID)
	if err != nil {
		return DurableView{}, fmt.Errorf("inspect: load node executions: %w", err)
	}
	view, err := Build(Request{Instance: inst, Nodes: nodes, Authorization: auth})
	if err != nil {
		return DurableView{}, err
	}

	l := &loader{ctx: ctx, ex: ex, req: req, auth: auth, inst: inst, nodes: nodes, c: &collector{}}
	out := DurableView{View: view}
	out.Records = append(out.Records, l.family(FamilyInstance, SectionInstance, 1))
	out.Records = append(out.Records, l.family(FamilyNodeExecution, SectionNode, len(nodes)))

	steps := []func(*DurableView) error{
		l.loadVersion, l.loadExecutionContext, l.loadTimersAndAttempts, l.loadReceipts,
		l.loadWorkItems, l.loadLease, l.loadCheckpoint, l.loadEffects,
	}
	for _, step := range steps {
		if err := step(&out); err != nil {
			return DurableView{}, err
		}
	}
	out.Records = append(out.Records,
		RecordFamily{Family: FamilyBusinessTransaction, Section: SectionTransaction, State: RecordUnavailable, Reason: ReasonNoBusinessTransactionStore},
		RecordFamily{Family: FamilyConnectorOperation, Section: SectionConnector, State: RecordUnavailable, Reason: ReasonNoConnectorOperationLink},
	)
	l.loadTrace(&out)

	out.Unavailable = []string{}
	for _, r := range out.Records {
		if r.State == RecordUnavailable && r.Reason != ReasonVersionRegistryNotConfigured {
			out.Unavailable = append(out.Unavailable, r.Family)
		}
	}
	sort.Strings(out.Unavailable)
	out.Completeness = mergeCompleteness(view.Completeness, out.WorkItems.Completeness, l.c.completeness())
	return out, nil
}

// loader carries one Load's inputs and its durable-read collector.
type loader struct {
	ctx   context.Context
	ex    dbport.Conn
	req   LoadRequest
	auth  Authorization
	inst  runtime.Instance
	nodes []runtime.NodeExecution
	c     *collector

	timers  []timer.Timer
	outboxT []string
}

// family builds a manifest line for a family read under section, recording a
// redaction when the section is denied.
func (l *loader) family(name string, section Section, count int) RecordFamily {
	if !l.auth.AllowsSection(section) {
		l.c.redact("records." + name)
		return RecordFamily{Family: name, Section: section, State: RecordRedacted, Reason: l.auth.sectionReason(section)}
	}
	state := RecordLoaded
	if count == 0 {
		state = RecordNotRecorded
	}
	return RecordFamily{Family: name, Section: section, State: state, Count: count}
}

func (l *loader) loadVersion(out *DurableView) error {
	pin := l.inst.CompiledPlanHash
	out.Version = VersionRecordView{CompiledPlanDigest: pin, Approvals: []VersionApprovalView{}}
	if !l.auth.AllowsSection(SectionDefinition) {
		out.Version = VersionRecordView{State: RecordRedacted, Reason: l.auth.sectionReason(SectionDefinition), Approvals: []VersionApprovalView{}}
		out.Records = append(out.Records, l.family(FamilyCompiledVersion, SectionDefinition, 0))
		return nil
	}
	if l.req.Versions == nil {
		out.Version.State, out.Version.Reason = RecordUnavailable, ReasonVersionRegistryNotConfigured
		l.c.gap("records.%s: no version registry configured to resolve pin %s", FamilyCompiledVersion, pin)
		out.Records = append(out.Records, RecordFamily{Family: FamilyCompiledVersion, Section: SectionDefinition,
			State: RecordUnavailable, Reason: ReasonVersionRegistryNotConfigured})
		return nil
	}
	cv, found, err := version.BindTx(l.ctx, l.ex, l.req.Versions).GetByDigest(pin)
	switch {
	case err != nil && version.CodeOf(err) == version.CodeRecordMutated:
		out.Version.State, out.Version.Reason = RecordIntegrityFailed, version.CodeRecordMutated
		l.c.gap("records.%s: pinned version %s fails its record digest", FamilyCompiledVersion, pin)
	case err != nil:
		return fmt.Errorf("inspect: load pinned compiled version: %w", err)
	case !found:
		out.Version.State = RecordNotRecorded
		l.c.gap("records.%s: pinned compiled plan %s is not in the version registry", FamilyCompiledVersion, pin)
	default:
		published := cv.PublishedAt.UTC()
		out.Version = VersionRecordView{
			State: RecordLoaded, CompiledPlanDigest: cv.CompiledPlanDigest, WorkflowID: cv.WorkflowID,
			DefinitionVersion: cv.DefinitionVersion, SemanticVersion: cv.SemanticVersion,
			DefinitionDigest: cv.DefinitionDigest, RecordDigest: cv.Digest(), Status: string(cv.Status),
			PublishedBy: cv.PublishedBy, PublishedAt: &published, Approvals: []VersionApprovalView{},
			PinMatches: cv.WorkflowID == l.inst.WorkflowID && cv.DefinitionVersion == l.inst.WorkflowVersion,
		}
		for _, a := range cv.Approvals {
			out.Version.Approvals = append(out.Version.Approvals, VersionApprovalView{
				Result: string(a.Result), ApprovedBy: a.ApprovedBy, Authority: a.Authority, ApprovedAt: a.ApprovedAt.UTC(),
			})
		}
		if !out.Version.PinMatches {
			l.c.gap("records.%s: registry record %s@%d does not match the instance pin %s@%d", FamilyCompiledVersion,
				cv.WorkflowID, cv.DefinitionVersion, l.inst.WorkflowID, l.inst.WorkflowVersion)
		}
	}
	count := 0
	if out.Version.State == RecordLoaded || out.Version.State == RecordIntegrityFailed {
		count = 1
	}
	out.Records = append(out.Records, RecordFamily{Family: FamilyCompiledVersion, Section: SectionDefinition,
		State: out.Version.State, Count: count, Reason: out.Version.Reason})
	return nil
}

func (l *loader) loadExecutionContext(out *DurableView) error {
	if !l.auth.AllowsSection(SectionInstance) {
		reason := l.auth.sectionReason(SectionInstance)
		out.ExecutionContext = ExecutionContextView{State: RecordRedacted, Reason: reason, ContextDigest: RefRedacted(reason)}
		out.Records = append(out.Records, l.family(FamilyExecutionContext, SectionInstance, 0))
		return nil
	}
	digest := protectedRef(l.inst.EffectiveContextRef, FieldInstanceContext, SectionInstance, l.auth, l.c)
	ec, found, err := runtime.LoadExecutionContext(l.ctx, l.ex, l.req.TenantID, l.req.InstanceID)
	view := ExecutionContextView{ContextDigest: digest}
	switch {
	case err != nil && runtime.CodeOf(err) == runtime.CodeContextDrift:
		view.State, view.Reason = RecordIntegrityFailed, runtime.CodeContextDrift
		l.c.gap("records.%s: stored execution context does not match the instance's pinned digest", FamilyExecutionContext)
	case err != nil:
		return fmt.Errorf("inspect: load execution context: %w", err)
	case !found:
		view.State = RecordNotRecorded
	default:
		view.State, view.Verified = RecordLoaded, true
		view.RuntimeVersion, view.ExecutionMode = ec.RuntimeVersion, string(ec.ExecutionMode)
		view.PlanDigestMatches = ec.CompiledPlanDigest == l.inst.CompiledPlanHash
		if !view.PlanDigestMatches {
			l.c.gap("records.%s: pinned context names plan %s, instance pins %s", FamilyExecutionContext,
				ec.CompiledPlanDigest, l.inst.CompiledPlanHash)
		}
	}
	out.ExecutionContext = view
	count := 0
	if view.State != RecordNotRecorded {
		count = 1
	}
	out.Records = append(out.Records, RecordFamily{Family: FamilyExecutionContext, Section: SectionInstance,
		State: view.State, Count: count, Reason: view.Reason})
	return nil
}

func (l *loader) loadTimersAndAttempts(out *DurableView) error {
	out.Timers, out.Attempts = []TimerView{}, []AttemptHistory{}
	if !l.auth.AllowsSection(SectionNode) {
		out.Records = append(out.Records, l.family(FamilyTimer, SectionNode, 0))
		return nil
	}
	timers, err := (timer.Scheduler{}).History(l.ctx, l.ex, l.req.TenantID, l.req.InstanceID)
	if err != nil {
		return fmt.Errorf("inspect: load timers: %w", err)
	}
	l.timers = timers
	for _, t := range timers {
		out.Timers = append(out.Timers, TimerView{
			TimerID: t.TimerID.String(), NodeID: t.NodeID, Kind: t.Kind, State: t.State, Key: t.Key,
			FiresAt: t.FiresAt.UTC(), Version: t.Version, CreatedAt: t.CreatedAt.UTC(),
		})
	}
	out.Records = append(out.Records, l.family(FamilyTimer, SectionNode, len(timers)))

	frontier := map[string]bool{}
	for _, id := range l.inst.CurrentNodeIDs {
		frontier[id] = true
	}
	byNode := map[string]*AttemptHistory{}
	order := []string{}
	for _, n := range l.nodes {
		h, ok := byNode[n.NodeID]
		if !ok {
			h = &AttemptHistory{NodeID: n.NodeID, Current: frontier[n.NodeID]}
			byNode[n.NodeID] = h
			order = append(order, n.NodeID)
		}
		h.Attempts++
		if n.Attempt >= h.LatestAttempt {
			h.LatestAttempt, h.LatestStatus = n.Attempt, string(n.Status)
			h.RetryPolicyRef = RefValue(n.Refs.RetryPolicyRef)
		}
	}
	for _, t := range timers {
		h, ok := byNode[t.NodeID]
		if !ok || t.Kind != runtimestate.TimerRetryBackoff {
			continue
		}
		h.RetryTimers++
		if t.State == runtimestate.TimerPending && (h.NextRetryAt == nil || t.FiresAt.Before(*h.NextRetryAt)) {
			at := t.FiresAt.UTC()
			h.NextRetryAt = &at
		}
	}
	sort.Strings(order)
	for _, id := range order {
		out.Attempts = append(out.Attempts, *byNode[id])
	}
	return nil
}

func (l *loader) loadReceipts(out *DurableView) error {
	out.Receipts = []ReceiptView{}
	if !l.auth.AllowsSection(SectionNode) {
		out.Records = append(out.Records, l.family(FamilyAdvancementReceipt, SectionNode, 0))
		return nil
	}
	records, err := runtime.LoadAdvancementReceipts(l.ctx, l.ex, l.req.TenantID, l.req.InstanceID)
	if err != nil {
		return fmt.Errorf("inspect: load advancement receipts: %w", err)
	}
	unverified := 0
	for _, r := range records {
		if !r.DigestVerified {
			unverified++
			l.c.gap("records.%s: receipt %s attempt %d (version %d) fails its digest", FamilyAdvancementReceipt,
				r.NodeID, r.Attempt, r.ResultingInstanceVersion)
		}
		out.Receipts = append(out.Receipts, ReceiptView{
			NodeID: r.NodeID, Attempt: r.Attempt,
			ExpectedInstanceVersion: r.ExpectedInstanceVersion, ResultingInstanceVersion: r.ResultingInstanceVersion,
			RequestDigest: r.RequestDigest, ReceiptDigest: r.StoredReceiptDigest, DigestVerified: r.DigestVerified,
			CompletedState: r.Receipt.CompletedState, RouteKey: r.Receipt.RouteKey,
			OutputDigest: protectedRef(r.Receipt.OutputDigest, FieldNodeOutput, SectionNode, l.auth, l.c),
			Frontier:     append([]string{}, r.Receipt.Frontier...),
			Complete:     r.Receipt.Complete, TerminalCode: r.Receipt.TerminalCode, RecordedAt: r.RecordedAt,
		})
	}
	rec := l.family(FamilyAdvancementReceipt, SectionNode, len(records))
	if unverified > 0 {
		rec.State = RecordIntegrityFailed
	}
	out.Records = append(out.Records, rec)
	return nil
}

func (l *loader) loadWorkItems(out *DurableView) error {
	if !l.req.WorkItems.Disclosed {
		out.WorkItems = BuildWorkItems(nil, nil, l.req.WorkItems)
		out.Records = append(out.Records, RecordFamily{Family: FamilyWorkItem, Section: SectionGovernance,
			State: RecordRedacted, Reason: l.req.WorkItems.DeniedReason})
		return nil
	}
	store := workitem.Store{}
	items, err := store.ListForInstance(l.ctx, l.ex, l.req.TenantID, l.req.InstanceID)
	if err != nil {
		return fmt.Errorf("inspect: list work items: %w", err)
	}
	transitions := make(map[string][]workitem.TransitionRecord, len(items))
	for _, item := range items {
		rows, err := store.LoadTransitions(l.ctx, l.ex, l.req.TenantID, item.WorkItemID)
		if err != nil {
			return fmt.Errorf("inspect: load work item transitions: %w", err)
		}
		transitions[item.WorkItemID.String()] = rows
	}
	out.WorkItems = BuildWorkItems(items, transitions, l.req.WorkItems)
	state := RecordLoaded
	if len(items) == 0 {
		state = RecordNotRecorded
	}
	out.Records = append(out.Records, RecordFamily{Family: FamilyWorkItem, Section: SectionGovernance, State: state, Count: len(items)})
	return nil
}

func (l *loader) loadLease(out *DurableView) error {
	out.Lease = LeaseView{Transitions: []LeaseTransitionView{}}
	if !l.auth.AllowsSection(SectionInstance) {
		out.Lease.State = RecordRedacted
		out.Records = append(out.Records, l.family(FamilyLease, SectionInstance, 0))
		return nil
	}
	history, err := (lease.Manager{}).History(l.ctx, l.ex, l.req.TenantID,
		lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: l.req.InstanceID.String()})
	if err != nil {
		return fmt.Errorf("inspect: load instance lease history: %w", err)
	}
	leases := map[uuid.UUID]bool{}
	for _, e := range history {
		leases[e.LeaseID] = true
		out.Lease.Transitions = append(out.Lease.Transitions, LeaseTransitionView{
			Kind: string(e.Kind), LeaseID: e.LeaseID.String(), HolderID: e.HolderID, Token: e.Token, At: e.At.UTC(),
		})
	}
	if n := len(history); n > 0 && history[n-1].Kind == lease.TransitionAcquired {
		out.Lease.Held, out.Lease.HolderID, out.Lease.Token = true, history[n-1].HolderID, history[n-1].Token
	}
	rec := l.family(FamilyLease, SectionInstance, len(leases))
	out.Lease.State = rec.State
	out.Records = append(out.Records, rec)
	return nil
}

func (l *loader) loadCheckpoint(out *DurableView) error {
	if !l.auth.AllowsSection(SectionInstance) {
		out.Checkpoint = CheckpointView{State: RecordRedacted}
		out.Records = append(out.Records, l.family(FamilyCheckpoint, SectionInstance, 0))
		return nil
	}
	cp, err := (runtimestate.CheckpointStore{}).Latest(l.ctx, l.ex, l.req.TenantID, l.req.InstanceID)
	switch {
	case errors.Is(err, runtimestate.ErrNotFound):
		out.Checkpoint = CheckpointView{State: RecordNotRecorded}
		if l.inst.LastCheckpointRef != "" {
			l.c.gap("records.%s: instance names checkpoint %s but none is stored", FamilyCheckpoint, l.inst.LastCheckpointRef)
		}
		out.Records = append(out.Records, l.family(FamilyCheckpoint, SectionInstance, 0))
		return nil
	case err != nil:
		return fmt.Errorf("inspect: load latest checkpoint: %w", err)
	}
	taken := cp.TakenAt.UTC()
	out.Checkpoint = CheckpointView{
		State: RecordLoaded, Sequence: cp.Sequence, Kind: cp.Kind, StateDigest: cp.StateDigest,
		FrontierDigest: cp.FrontierDigest, VariableDigest: cp.VariableDigest,
		InstanceVersion: cp.InstanceVersion, TakenAt: &taken,
	}
	out.Records = append(out.Records, l.family(FamilyCheckpoint, SectionInstance, 1))
	return nil
}

// loadEffects follows every effect reference a node recorded to the outbox
// row and the reconciliation jobs keyed by the same effect identity.
func (l *loader) loadEffects(out *DurableView) error {
	out.Effects = []EffectView{}
	if !l.auth.AllowsSection(SectionConnector) {
		out.Records = append(out.Records, l.family(FamilyOutbox, SectionConnector, 0))
		out.Records = append(out.Records, l.family(FamilyReconciliation, SectionObservation, 0))
		return nil
	}
	nodesByRef := map[string]map[string]bool{}
	for _, n := range l.nodes {
		for _, ref := range n.Refs.EffectRefs {
			if nodesByRef[ref] == nil {
				nodesByRef[ref] = map[string]bool{}
			}
			nodesByRef[ref][n.NodeID] = true
		}
	}
	refs := make([]string, 0, len(nodesByRef))
	for ref := range nodesByRef {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	observationAllowed := l.auth.AllowsSection(SectionObservation)
	outboxRows, jobs := 0, 0
	for _, ref := range refs {
		ev := EffectView{EffectRef: ref, NodeIDs: sortedKeys(nodesByRef[ref]), OutboxState: RecordNotRecorded,
			Reconciliation: []ReconciliationView{}}
		rec, found, err := outbox.ReadByEffectIdentity(l.ctx, l.ex, l.req.TenantID, ref)
		if err != nil {
			return fmt.Errorf("inspect: read outbox for effect %s: %w", ref, err)
		}
		if found {
			outboxRows++
			ev.OutboxState = RecordLoaded
			ev.Outbox = &OutboxView{
				OutboxID: rec.OutboxID.String(), Status: rec.Status, Attempts: rec.Attempts,
				OrderingKey: rec.OrderingKey, SchemaRef: rec.SchemaRef, Criticality: rec.Criticality,
				AvailableAt: rec.AvailableAt.UTC(), UpdatedAt: rec.UpdatedAt.UTC(), LastErrorRecorded: rec.LastError != nil,
			}
			if rec.Causal != nil && rec.Causal.TraceLink != nil {
				l.outboxT = append(l.outboxT, rec.Causal.TraceLink.TraceID)
			}
		}
		if !observationAllowed {
			ev.ReconciliationState = RecordRedacted
		} else {
			list, err := (reconcile.PostgresStore{}).ListForEffect(l.ctx, l.ex, l.req.TenantID, ref)
			if err != nil {
				return fmt.Errorf("inspect: list reconciliation jobs for effect %s: %w", ref, err)
			}
			ev.ReconciliationState = RecordNotRecorded
			if len(list) > 0 {
				ev.ReconciliationState = RecordLoaded
			}
			jobs += len(list)
			for _, j := range list {
				ev.Reconciliation = append(ev.Reconciliation, ReconciliationView{
					JobID: j.JobID.String(), PolicyRef: j.PolicyRef, Status: string(j.Status),
					IntendedRef: j.IntendedRef, CanonicalRef: j.CanonicalRef,
					RequiredFreshness: string(j.RequiredFreshness), ObservationAttempts: j.ObservationAttempts,
					NextCheckAt: j.NextCheckAt.UTC(), Deadline: j.Deadline.UTC(), RepairPolicy: j.RepairPolicy, Owner: j.Owner,
				})
			}
		}
		out.Effects = append(out.Effects, ev)
	}
	out.Records = append(out.Records, l.family(FamilyOutbox, SectionConnector, outboxRows))
	out.Records = append(out.Records, l.family(FamilyReconciliation, SectionObservation, jobs))
	return nil
}

// loadTrace collects every trace id the durable records carry: node
// executions, timer and outbox trace links. Trace ids explain software
// behavior and are never business evidence.
func (l *loader) loadTrace(out *DurableView) {
	if !l.auth.AllowsSection(SectionTrace) {
		out.TraceIDs = RefListRedacted(l.auth.sectionReason(SectionTrace))
		out.Records = append(out.Records, l.family(FamilyTrace, SectionTrace, 0))
		return
	}
	ids := map[string]bool{}
	for _, n := range l.nodes {
		if n.TraceID != "" {
			ids[n.TraceID] = true
		}
	}
	for _, t := range l.timers {
		if t.Causal != nil && t.Causal.TraceLink != nil && t.Causal.TraceLink.TraceID != "" {
			ids[t.Causal.TraceLink.TraceID] = true
		}
	}
	for _, id := range l.outboxT {
		if id != "" {
			ids[id] = true
		}
	}
	out.TraceIDs = RefListValue(sortedKeys(ids))
	out.Records = append(out.Records, l.family(FamilyTrace, SectionTrace, len(ids)))
}

// mergeCompleteness folds several sections' completeness into one.
func mergeCompleteness(parts ...Completeness) Completeness {
	c := &collector{}
	for _, p := range parts {
		for _, r := range p.Redactions {
			c.redact(r)
		}
		for _, g := range p.Gaps {
			c.gap("%s", g)
		}
	}
	return c.completeness()
}
