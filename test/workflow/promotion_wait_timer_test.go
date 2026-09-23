package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepswait "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// The WF-RUN-002 + WF-RUN-004 end-to-end: a promotion instance runs through a
// real WAIT step, parks on a durable timer, is woken by a caller-driven fire
// under a lease fence, and reaches its governed terminal write.
//
// Everything here is composed from the real packages: the plan goes through
// the real compiler and version registry, the wake instant is resolved by
// internal/workflow/steps/wait's pure ComputeTimerRequirement, the promise is
// a workflow_timer row written by internal/workflow/timer, the fence is a
// workflow_lease row held through internal/workflow/lease, and the terminal
// write is effects.LedgerTerminalWriter. Nothing sleeps and nothing polls:
// every instant in the test is one the test itself supplies.

const (
	waitWorkflowID = "hcmnext.workflows.test.promotion_wait_demo"

	nodeWaitPrepare   = "prepare_promotion"
	nodeWaitEffective = "wait_effective_date"
	nodeWaitApplied   = "end_wait_applied"
	nodeWaitExpired   = "end_wait_expired"
	nodeWaitCancelled = "end_wait_cancelled"
	nodeWaitDegraded  = "end_wait_degraded"

	waitTerminalCode = "PROMOTION_APPLIED_AFTER_WAIT"

	waitZoneID     = "America/New_York"
	waitTzdb       = "2026a"
	waitCalendar   = "us-federal"
	waitCalendarV  = "2026.1"
	waitTimerQueue = "queue:workflow-runtime"
)

// waitDataset is the tzdb and calendar release the wake condition is resolved
// against. It is supplied, never read from the environment.
var waitDataset = values.DatasetVersions{TzdbVersion: waitTzdb, CalendarVersion: waitCalendarV}

// promotionWaitDefinition is the smallest executable graph that parks on a
// durable timer: a trivial TRANSFORM (frontier.Seed always marks the start
// node READY, so a WAIT may not be the start node of a driven plan), then a
// WAIT on a fixed business instant, then one of four terminals.
func promotionWaitDefinition(fireAt time.Time) workflow.Definition {
	return workflow.Definition{
		WorkflowID: waitWorkflowID, Version: 1, Name: "Promotion wait demo (test/workflow)",
		InputSchema: demoSchema("WaitInput"), OutputSchema: demoSchema("WaitResult"),
		VariablesSchema: demoSchema("WaitVariables"),
		TenantScope:     "test", OrganizationScope: humanwork.ScenarioOrganizationScopeID, RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID: nodeWaitPrepare,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "proposal_digest", Type: workflow.ValueType{Kind: workflow.KindString}},
		},
		Outputs:               demoTerminalFields(),
		Limits:                workflow.Limits{MaxFanOut: 5, MaxDepth: 3, MaxNodes: 8},
		FailurePolicyRef:      "policy.workflow.failure.test/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.test/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.test/v1",
		Nodes: []workflow.Node{
			{
				ID: nodeWaitPrepare, Type: workflow.StepTransform,
				InputSchema: demoSchema("WaitPrepareInput"), OutputSchema: demoSchema("WaitPrepareResult"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "proposal_digest", Type: workflow.ValueType{Kind: workflow.KindString}},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "proposal_digest", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "proposal_digest"}},
				},
				Transform: &workflow.TransformSpec{
					TransformRef: "transform.test.promotion-wait-demo.prepare/v1", Version: 1,
					NormalizationProfile: "profile.test.promotion-wait-demo/v1", OutputTaint: workflow.TaintDerived,
					Limits: workflow.TransformLimits{
						MaxInputBytes: workflow.MaxTransformInputBytes, MaxOutputBytes: workflow.MaxTransformOutputBytes,
						MaxSteps: workflow.MaxTransformSteps,
					},
				},
				DeclaredEffect: capability.EffectPure,
				Governance: workflow.NodeGovernance{
					Purpose: "PROMOTION_WAIT_DEMO", Classification: "CONFIDENTIAL_HR",
					RevalidationBoundary:  workflow.RevalidatePreExecution,
					DataAccessManifestRef: "data-access.test.promotion-wait-demo/v1",
				},
			},
			{
				ID: nodeWaitEffective, Type: workflow.StepWait,
				InputSchema: demoSchema("WaitEffectiveInput"), OutputSchema: demoSchema("WaitEffectiveResult"),
				Inputs: []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
				},
				DeclaredEffect: capability.EffectPure,
				// The whole dataset provenance of the wake instant is declared
				// here, on the immutable compiled node: zone, tzdb release,
				// business calendar and what happens when either is
				// republished. The durable timer pins it by digest.
				Wait: &workflow.WaitSpec{
					WakeKind:              workflow.WaitWakeAtInstant,
					WakeInstant:           fireAt.UTC().Format(time.RFC3339Nano),
					ZoneID:                waitZoneID,
					ZoneTzdbVersion:       waitTzdb,
					CalendarRef:           waitCalendar,
					CalendarVersion:       waitCalendarV,
					ReferenceUpdatePolicy: "PIN",
				},
				FailureRoute: nodeWaitDegraded,
				Governance: workflow.NodeGovernance{
					Purpose: "PROMOTION_EFFECTIVE_DATE", Classification: "CONFIDENTIAL_HR",
					RevalidationBoundary:  workflow.RevalidatePreExecution,
					DataAccessManifestRef: "data-access.test.promotion-wait-demo/v1",
				},
			},
			demoTerminalNode(nodeWaitApplied, waitTerminalCode, workflow.RuntimeCompleted,
				demoCompletion("CLOSED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
				"receipt.test.promotion-wait-demo/v1"),
			demoTerminalNode(nodeWaitExpired, "EXPIRED", workflow.RuntimeCancelled,
				demoCompletion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
			demoTerminalNode(nodeWaitCancelled, "CANCELLED", workflow.RuntimeCancelled,
				demoCompletion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
			// The WAIT node's declared failure route: a wake condition that could
			// not be trusted to fire (WF-STEP-005's TIMER_REVIEW_REQUIRED) ends
			// here rather than guessing an instant.
			demoTerminalNode(nodeWaitDegraded, "TIMER_REVIEW_REQUIRED", workflow.RuntimeCompleted,
				demoCompletion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
		},
		Edges: []workflow.Edge{
			{From: nodeWaitPrepare, To: nodeWaitEffective, RouteKey: "SUCCEEDED"},
			{From: nodeWaitPrepare, To: nodeWaitDegraded, RouteKey: "FAILED"},
			{From: nodeWaitEffective, To: nodeWaitApplied, RouteKey: "SUCCEEDED"},
			{From: nodeWaitEffective, To: nodeWaitExpired, RouteKey: "LATE"},
			{From: nodeWaitEffective, To: nodeWaitCancelled, RouteKey: "CANCELLED"},
		},
	}
}

func publishActiveWaitPlan(t *testing.T, fireAt, at time.Time) (*version.Registry, *workflow.CompiledWorkflow, version.CompiledVersion) {
	t.Helper()
	def := promotionWaitDefinition(fireAt)
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile the promotion wait definition: %v", err)
	}
	store := version.NewRegistry()
	published, err := version.Publish(store, def, plan, workflow.Options{Phase: workflow.PhaseP1B}, version.PublishMeta{
		SemanticVersion: "1.0.0", PublishedAt: at, PublishedBy: "test:promotion-wait",
	})
	if err != nil {
		t.Fatalf("version.Publish: %v", err)
	}
	activated, err := version.Activate(store, published.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "test:release", Authority: "authority:release",
		ApprovedAt: at, ReviewedPlanDigest: published.CompiledPlanDigest, TestsPassed: true,
	})
	if err != nil {
		t.Fatalf("version.Activate: %v", err)
	}
	return store, plan, activated
}

// ---------------------------------------------------------------------------
// The two adapters a composition root would own: they translate between
// internal/workflow/execute's ports and internal/workflow/timer's API. They
// live here rather than in either package because neither should depend on
// the other; internal/platform/execution is where the production pair
// belongs once a service wires this graph for real.
// ---------------------------------------------------------------------------

type waitTimerFactory struct {
	scheduler timer.Scheduler
	dataset   values.DatasetVersions
	// requirements records the wake requirement each node's promise was
	// minted from, so the test can later resolve exactly that requirement
	// rather than recomputing one and hoping it matches.
	requirements map[string]stepswait.TimerRequirement
}

func (f *waitTimerFactory) CreateTimer(ctx context.Context, ex runtime.Executor, req execute.TimerRequest) (execute.TimerHandle, error) {
	node, err := stepswait.FromCompiled(&req.Node)
	if err != nil {
		return execute.TimerHandle{}, fmt.Errorf("bind the compiled WAIT node: %w", err)
	}
	node.WorkflowID, node.WorkflowVersion = req.Plan.WorkflowID, req.Plan.Version

	requirement, err := stepswait.ComputeTimerRequirement(node, f.dataset)
	if err != nil {
		return execute.TimerHandle{}, fmt.Errorf("compute the wake requirement: %w", err)
	}
	scheduled, err := f.scheduler.Schedule(ctx, ex, timer.Request{
		TenantID: req.Continuation.TenantID, InstanceID: req.Continuation.InstanceID,
		NodeID: req.Continuation.TargetNodeID, Kind: timer.KindUntil,
		Requirement: requirement, CreatedAt: req.CreatedAt,
	})
	if err != nil {
		return execute.TimerHandle{}, err
	}
	if f.requirements == nil {
		f.requirements = map[string]stepswait.TimerRequirement{}
	}
	f.requirements[req.Continuation.TargetNodeID] = requirement
	return execute.TimerHandle{
		TimerID: scheduled.Timer.TimerID, NodeID: scheduled.Timer.NodeID,
		Key: scheduled.Timer.Key, FiresAt: scheduled.Timer.FiresAt, Replay: scheduled.Replay,
	}, nil
}

type waitTimerReader struct{ scheduler timer.Scheduler }

func (r waitTimerReader) LoadTimer(ctx context.Context, ex runtime.Executor, tenantID, timerID uuid.UUID) (execute.FiredTimer, error) {
	row, err := r.scheduler.Load(ctx, ex, tenantID, timerID)
	if err != nil {
		return execute.FiredTimer{}, err
	}
	return execute.FiredTimer{
		TimerID: row.TimerID, InstanceID: row.InstanceID, NodeID: row.NodeID,
		Key: row.Key, State: row.State, FiresAt: row.FiresAt,
	}, nil
}

// ---------------------------------------------------------------------------

func TestPromotionWorkflowWaitsOnARealTimerAndCompletesUnderALeaseFence(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	fireAt := at.Add(48 * time.Hour)

	db := pgtest.New(t)
	beginner := appConn(t, db)
	tenantID := insertTenant(t, db, "promo-wait-1", at)

	versions, plan, activated := publishActiveWaitPlan(t, fireAt, at)
	proposal := newDemoProposal(t, values.TenantId("promo-wait-1"), "intent:promotion-wait-demo-1", at)
	binding := runtime.ProposalBinding{Revision: proposal, ApprovalRef: "decision:hr-partner-approves-start"}

	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: activated.CompiledPlanDigest}, Plan: plan,
	}}}
	terminal := &effects.LedgerTerminalWriter{
		Appender: newLedgerAppender(t), ProjectionName: "workflow_promotion_wait_outcome_test",
		SourceRef: "hcmnext:test:workflow",
	}

	// Timer scheduling holds a queue lease. Instance advancement has its own
	// WORKFLOW_INSTANCE lease: queue ownership must not authorize a write to
	// an arbitrary instance.
	holderA := lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:wait-a"}
	holderB := lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:wait-b"}
	queue := lease.Resource{Kind: lease.ResourceQueue, ID: waitTimerQueue}
	var manager lease.Manager
	var grantA lease.Grant
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		grantA, err = manager.Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: tenantID, Resource: queue, Holder: holderA, Now: at, TTL: time.Hour,
		})
		return err
	})

	scheduler := timer.Scheduler{}
	factory := &waitTimerFactory{scheduler: scheduler, dataset: waitDataset}
	instanceLeaserA := platformexecution.NewInstanceLeaser(holderA, time.Hour)

	drvA, err := execute.New(execute.Options{
		DB: beginner, Steps: endOnlySteps{}, Terminal: terminal,
		Timers: factory, TimerReader: waitTimerReader{scheduler: scheduler},
		Guard:     idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:     func() time.Time { return at },
		Leases:    instanceLeaserA, FenceVerifier: lease.Fenced{Manager: manager},
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}

	start := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:promotion-wait-demo-1",
		Resolver: resolver, Versions: versions, Proposal: binding,
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(proposal),
		ExpectedIntentID: proposal.IntentID, ExpectedTenant: proposal.Tenant,
		BusinessSubjectRefs: []string{"employment:promotion-execute-demo-1"},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "corr:promotion-wait-demo-1", CreatedAt: at,
	}

	// --- Execute: runs the TRANSFORM and parks on the durable timer. ---
	parked, err := drvA.Execute(ctx, execute.ExecuteRequest{Start: start})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if parked.Status != execute.StatusParked {
		t.Fatalf("Execute status = %s, want PARKED on a timer", parked.Status)
	}
	// Model a subsequent worker claim that dies without releasing its lease.
	var instanceFenceA, instanceFenceB runtime.Fence
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		instanceFenceA, err = instanceLeaserA.AcquireInstance(ctx, tx, tenantID, parked.Start.InstanceID, at)
		return err
	})
	if len(parked.Timers) != 1 {
		t.Fatalf("Execute created %d timers, want exactly 1", len(parked.Timers))
	}
	if len(parked.WorkItems) != 0 {
		t.Fatalf("a WAIT step created %d work items; parking on a timer is not human work", len(parked.WorkItems))
	}
	promise := parked.Timers[0]
	if promise.NodeID != nodeWaitEffective || !promise.FiresAt.Equal(fireAt) || promise.Replay {
		t.Fatalf("durable timer = %+v, want a fresh promise for %s at %s", promise, nodeWaitEffective, fireAt)
	}
	requirement, ok := factory.requirements[nodeWaitEffective]
	if !ok {
		t.Fatal("the timer factory recorded no wake requirement")
	}
	if promise.Key != requirement.Digest {
		t.Fatalf("timer key = %q, want the wake requirement digest %q", promise.Key, requirement.Digest)
	}

	// The promise is a durable row, not a Go value the driver kept.
	var storedState, storedKey string
	var storedFires time.Time
	db.QueryRow(ctx, `
		SELECT timer_state, timer_key, fires_at FROM workflow_timer WHERE tenant_id = $1 AND timer_id = $2`,
		tenantID, promise.TimerID).Scan(&storedState, &storedKey, &storedFires)
	if storedState != "PENDING" || storedKey != requirement.Digest || !storedFires.UTC().Equal(fireAt) {
		t.Fatalf("stored timer = %s/%s/%s, want PENDING at the wake instant under the requirement digest",
			storedState, storedKey, storedFires)
	}

	// --- The worker dies and a second replica takes the queue lease. ---
	takeoverAt := at.Add(2 * time.Hour)
	var grantB lease.Grant
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var aerr error
		grantB, aerr = manager.Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: tenantID, Resource: queue, Holder: holderB, Now: takeoverAt, TTL: 720 * time.Hour,
		})
		return aerr
	})
	if grantB.Fence.Token != grantA.Fence.Token+1 {
		t.Fatalf("takeover fence token = %d, want %d", grantB.Fence.Token, grantA.Fence.Token+1)
	}
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		instanceFenceB, err = platformexecution.NewInstanceLeaser(holderB, 720*time.Hour).AcquireInstance(ctx, tx, tenantID, parked.Start.InstanceID, takeoverAt)
		return err
	})
	if instanceFenceB.Token <= instanceFenceA.Token {
		t.Fatalf("instance takeover token = %d, want greater than %d", instanceFenceB.Token, instanceFenceA.Token)
	}

	// --- The new holder fires the due promise. Nothing polls: the caller
	//     supplies the instant and the misfire policy. ---
	var fired timer.FireResult
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var ferr error
		fired, ferr = scheduler.Fire(ctx, tx, timer.FireRequest{
			TenantID: tenantID, Now: fireAt, Fence: grantB.Fence,
			Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour},
		})
		return ferr
	})
	if len(fired.Fired) != 1 || fired.Fired[0].Timer.TimerID != promise.TimerID {
		t.Fatalf("fire result = %+v, want the one promise settled", fired)
	}
	if fired.Fired[0].Decision != timer.DecisionOnTime {
		t.Fatalf("misfire decision = %q, want ON_TIME for a timer fired at its own instant", fired.Fired[0].Decision)
	}
	var readyState string
	var eligibleAt time.Time
	db.QueryRow(ctx, `
		SELECT ready_state, eligible_at FROM workflow_ready_work WHERE tenant_id = $1 AND ready_work_id = $2`,
		tenantID, fired.Fired[0].ReadyWorkID).Scan(&readyState, &eligibleAt)
	if readyState != "READY" || !eligibleAt.UTC().Equal(fireAt) {
		t.Fatalf("woken work = %s at %s, want READY at the wake instant", readyState, eligibleAt)
	}

	// --- The typed WAIT resolution is computed purely, from the exact
	//     requirement the promise was minted from. ---
	resolution, err := stepswait.Resolve(requirement, values.NewInstant(fireAt), stepswait.WakeEvent{Kind: stepswait.EventWake})
	if err != nil {
		t.Fatalf("wait.Resolve: %v", err)
	}
	if resolution.Outcome != stepswait.OutcomeFired {
		t.Fatalf("wait resolution = %q, want FIRED", resolution.Outcome)
	}
	waitOutcome := resolution.ToNodeOutcome(nodeWaitEffective)
	waitOutcome.OutputDigest = "sha256:" + requirement.Digest[:32] + requirement.Digest[:32]

	resumeAt := fireAt.Add(time.Minute)

	// --- WF-RUN-002 in the composed system: the superseded holder cannot
	//     advance the instance, however correct its resolution is. ---
	staleResume := execute.ResumeTimerRequest{
		Start: start, InstanceID: parked.Start.InstanceID, ExpectedInstanceVersion: parked.InstanceVersion,
		TimerID: promise.TimerID, Outcome: waitOutcome, RecordedAt: resumeAt,
	}
	if _, err := drvA.ResumeTimer(execute.WithFence(ctx, instanceFenceA), staleResume); err == nil {
		t.Fatal("the superseded holder advanced the instance")
	} else {
		if !errors.Is(err, execute.ErrFenceRefused) {
			t.Fatalf("stale-fence resume: err = %v, want ErrFenceRefused", err)
		}
		if !errors.Is(err, lease.ErrFenceStale) && !errors.Is(err, lease.ErrLeaseLost) {
			t.Fatalf("stale-fence resume does not carry the lease refusal: %v", err)
		}
	}
	var versionAfterRefusal int64
	db.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, parked.Start.InstanceID).Scan(&versionAfterRefusal)
	if versionAfterRefusal != parked.InstanceVersion {
		t.Fatalf("a refused advancement moved the instance from version %d to %d",
			parked.InstanceVersion, versionAfterRefusal)
	}

	// --- The live holder resumes from the fired timer and reaches the
	//     governed terminal write. ---
	fenceB := instanceFenceB
	drvB, err := execute.New(execute.Options{
		DB: beginner, Steps: endOnlySteps{}, Terminal: terminal,
		Timers: factory, TimerReader: waitTimerReader{scheduler: scheduler},
		Guard:     idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:     func() time.Time { return resumeAt },
		Fence:     &fenceB, FenceVerifier: lease.Fenced{Manager: manager},
	})
	if err != nil {
		t.Fatalf("execute.New (successor holder): %v", err)
	}

	completed, err := drvB.ResumeTimer(ctx, staleResume)
	if err != nil {
		t.Fatalf("ResumeTimer under the live fence: %v", err)
	}
	if completed.Status != execute.StatusComplete {
		t.Fatalf("ResumeTimer status = %s, want COMPLETE", completed.Status)
	}
	if len(completed.Advances) != 2 {
		t.Fatalf("%d advancements, want two: the WAIT node and the END node", len(completed.Advances))
	}
	last := completed.Advances[len(completed.Advances)-1]
	if !last.Complete || last.TerminalCode != waitTerminalCode {
		t.Fatalf("terminal advancement = %+v, want %s", last, waitTerminalCode)
	}

	// --- One governed business fact, and the promise is settled exactly
	//     once behind it. ---
	var ledgerEvents int
	db.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenantID).Scan(&ledgerEvents)
	if ledgerEvents != 1 {
		t.Fatalf("%d ledger events, want exactly 1 governed business fact", ledgerEvents)
	}
	var timerState string
	var timerVersion int64
	db.QueryRow(ctx, `SELECT timer_state, timer_version FROM workflow_timer WHERE tenant_id = $1 AND timer_id = $2`,
		tenantID, promise.TimerID).Scan(&timerState, &timerVersion)
	if timerState != "FIRED" || timerVersion != 2 {
		t.Fatalf("settled timer = %s at version %d, want FIRED at version 2", timerState, timerVersion)
	}
	var readyRows int
	db.QueryRow(ctx, `SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, parked.Start.InstanceID).Scan(&readyRows)
	if readyRows != 1 {
		t.Fatalf("%d ready-work rows, want exactly 1", readyRows)
	}

	// --- Nothing is left promising to wake a completed instance. ---
	var pending []timer.Timer
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var perr error
		pending, perr = scheduler.Pending(ctx, tx, tenantID, parked.Start.InstanceID)
		return perr
	})
	if len(pending) != 0 {
		t.Fatalf("%d timers are still pending against a completed instance", len(pending))
	}
}
