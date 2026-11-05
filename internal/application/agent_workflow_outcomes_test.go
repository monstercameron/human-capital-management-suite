package application

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/workflowbridge"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// workflowOutcomeHarness shares one core/agent database pair across outcome
// branches. Each branch owns a distinct tenant and workflow instance, so one
// branch can never observe another branch's signal or continuation.
type workflowOutcomeHarness struct {
	core      *pgtest.DB
	agents    *pgtest.DB
	coreConn  *pgxadapter.Conn
	agentConn *pgxadapter.Conn
	tenants   map[string]uuid.UUID
	now       time.Time
	deliverAt time.Time
}

type workflowOutcomeBranch struct {
	ref      string
	tenant   uuid.UUID
	instance uuid.UUID
	inbox    *agentrunstore.AdmissionRepository
	state    *agentrunstate.TenantStore
	exec     *runstate.Service
	record   agentrun.Record
	bridge   AgentWorkflowCompletionBridge
}

func newWorkflowOutcomeHarness(t *testing.T, deliverAt time.Time) *workflowOutcomeHarness {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	core := pgtest.New(t)
	agents := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agents.SQL); err != nil {
		t.Fatal(err)
	}
	coreConn := core.NewConn(t)
	if _, err := coreConn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	agentConn := agents.NewConn(t)
	if _, err := agentConn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	return &workflowOutcomeHarness{core: core, agents: agents, coreConn: coreConn, agentConn: agentConn,
		tenants: map[string]uuid.UUID{}, now: now, deliverAt: deliverAt}
}

func (h *workflowOutcomeHarness) mapper(ref string) uuid.UUID { return h.tenants[ref] }

func (h *workflowOutcomeHarness) admit(t *testing.T, ref string, revoked bool, deadline time.Time) *workflowOutcomeBranch {
	t.Helper()
	ctx := context.Background()
	tenant, instance := uuid.New(), uuid.New()
	h.tenants[ref] = tenant
	h.core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,$2,'cell-local','Workflow agents','ACTIVE',$3)`, tenant, ref, h.now.Add(-time.Hour))
	h.agents.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant)
	h.core.Exec(t, `INSERT INTO workflow_instance(tenant_id,instance_id,cell_id,workflow_id,workflow_version,compiled_plan_hash,business_subject_refs,execution_mode,runtime_status,completion_dimensions,input_ref,variable_revision_head,current_node_ids,correlation_id,created_at) VALUES($1,$2,'cell-local','workflow-agent-test',1,repeat('a',64),'{}','EXECUTE','WAITING','{}','input:one',0,'{await_agent}','workflow-agent-test',$3)`, tenant, instance, h.now)
	runner := workflowAgentTenantRunner{conn: h.agentConn}
	inbox, err := agentrunstore.NewAdmissionRepository(runner, tenant, values.TenantId(ref))
	if err != nil {
		t.Fatal(err)
	}
	states, err := agentrunstate.New(runner, h.mapper)
	if err != nil {
		t.Fatal(err)
	}
	state, err := states.ForTenant(ref)
	if err != nil {
		t.Fatal(err)
	}
	request := commonAgentTestRequest(h.now)
	request.Deadline = deadline
	request.Source = agentrun.SourceIdentity{TenantID: ref, Kind: agentrun.SourceWorkflow, Ref: "workflow:" + instance.String() + ":analyze"}
	request.Source, err = (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Store: inbox, Authority: &commonAgentTestAuthority{revoked: revoked}, Now: func() time.Time { return h.now }})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := admission.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := runstate.New(state, workflowAgentRecheck{})
	if err != nil {
		t.Fatal(err)
	}
	bridge := AgentWorkflowCompletionBridge{Evidence: workflowAgentEvidence{tenant: ref, inbox: inbox, state: state},
		Current: workflowAgentRecheck{}, Core: h.coreConn, ResolveTenant: h.mapper, Now: func() time.Time { return h.deliverAt }}
	return &workflowOutcomeBranch{ref: ref, tenant: tenant, instance: instance, inbox: inbox, state: state, exec: execution, record: record, bridge: bridge}
}

func (h *workflowOutcomeHarness) subscribe(t *testing.T, branch *workflowOutcomeBranch, runID string) {
	t.Helper()
	runner := workflowAgentTenantRunner{conn: h.coreConn}
	if err := runner.RunTenantTx(context.Background(), branch.tenant, func(tx dbport.Tx) error {
		return (signals.Store{}).Subscribe(context.Background(), tx, signals.Subscription{TenantID: branch.tenant, SubscriptionID: uuid.New(),
			InstanceID: branch.instance, NodeID: "await_agent", NodeAttempt: 1, EventType: workflowbridge.EventType,
			CorrelationKey: workflowbridge.CorrelationKey, CorrelationValue: runID, ExpectedSchemaRef: workflowbridge.SchemaRef,
			AcceptedSources: []string{workflowbridge.Source}, Ordering: stepSignal.OrderingNone, CreatedAt: h.now, ClosesAt: h.deliverAt.Add(time.Hour)})
	}); err != nil {
		t.Fatal(err)
	}
}

func (h *workflowOutcomeHarness) completionPayload(t *testing.T, branch *workflowOutcomeBranch, runID string) workflowbridge.Result {
	t.Helper()
	var raw string
	if err := h.core.QueryRow(context.Background(), `SELECT payload::text FROM workflow_signal WHERE tenant_id=$1 AND dedupe_token=$2`, branch.tenant, runID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result workflowbridge.Result
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func (h *workflowOutcomeHarness) continuations(t *testing.T, branch *workflowOutcomeBranch) int {
	t.Helper()
	var count int
	if err := h.core.QueryRow(context.Background(), `SELECT count(*) FROM workflow_ready_work WHERE tenant_id=$1 AND instance_id=$2`, branch.tenant, branch.instance).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// TestTodo_AGENT_033_OutcomeBranches_Integration proves every GREEN branch end
// to end: a refused admission and a failed, cancelled, or expired run each
// deliver a typed completion signal that wakes exactly one workflow wait with
// no output disclosure on non-success outcomes.
func TestTodo_AGENT_033_OutcomeBranches_Integration(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	harness := newWorkflowOutcomeHarness(t, now.Add(3*time.Second))
	ctx := context.Background()
	for _, tc := range []struct {
		name        string
		revoked     bool
		terminal    func(t *testing.T, branch *workflowOutcomeBranch) string
		wantOutcome workflowbridge.Outcome
		wantCode    string
	}{
		{name: "refused", revoked: true,
			terminal:    func(_ *testing.T, branch *workflowOutcomeBranch) string { return branch.record.ID },
			wantOutcome: workflowbridge.Refused, wantCode: "SPONSOR_REVOKED"},
		{name: "failed",
			terminal: func(t *testing.T, branch *workflowOutcomeBranch) string {
				run, err := branch.exec.Start(ctx, branch.record)
				if err != nil {
					t.Fatal(err)
				}
				run, err = branch.exec.Claim(ctx, run.ID, "agent-worker", now, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				run, err = branch.exec.Fail(ctx, run.ID, "agent-worker", "MODEL_TIMEOUT", false, run.Fence, run.Version, now.Add(time.Second))
				if err != nil {
					t.Fatal(err)
				}
				return run.ID
			},
			wantOutcome: workflowbridge.Failed, wantCode: "MODEL_TIMEOUT"},
		{name: "cancelled",
			terminal: func(t *testing.T, branch *workflowOutcomeBranch) string {
				run, err := branch.exec.Start(ctx, branch.record)
				if err != nil {
					t.Fatal(err)
				}
				run, err = branch.exec.Cancel(ctx, run.ID, run.Version, now.Add(time.Second))
				if err != nil {
					t.Fatal(err)
				}
				return run.ID
			},
			wantOutcome: workflowbridge.Cancelled, wantCode: "CANCELLED"},
		{name: "expired",
			terminal: func(t *testing.T, branch *workflowOutcomeBranch) string {
				run, err := branch.exec.Start(ctx, branch.record)
				if err != nil {
					t.Fatal(err)
				}
				run, err = branch.exec.Expire(ctx, run.ID, run.Version, branch.record.Request.Deadline)
				if err != nil {
					t.Fatal(err)
				}
				return run.ID
			},
			wantOutcome: workflowbridge.Expired, wantCode: "EXPIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			branch := harness.admit(t, "outcome-"+tc.name, tc.revoked, now.Add(time.Hour))
			runID := tc.terminal(t, branch)
			harness.subscribe(t, branch, runID)
			receipt, err := branch.bridge.Deliver(ctx, branch.ref, runID)
			if err != nil || len(receipt.Dispositions) != 1 || receipt.Dispositions[0].Status != stepSignal.StatusAccepted {
				t.Fatalf("completion=%+v err=%v", receipt, err)
			}
			result := harness.completionPayload(t, branch, runID)
			if result.Outcome != tc.wantOutcome || result.Code != tc.wantCode || result.RunID != runID || result.Agent != branch.record.Request.Agent || result.OutputRef != "" || result.OutputDigest != "" {
				t.Fatalf("completion payload=%+v", result)
			}
			if got := harness.continuations(t, branch); got != 1 {
				t.Fatalf("durable continuations=%d want 1", got)
			}
		})
	}
}

// TestTodo_AGENT_033_TimeoutSweep_Recovery proves the GREEN timeout branch: a
// run the workers never finished past its admission deadline is expired by
// recovery and wakes its workflow wait with a typed EXPIRED outcome instead
// of stranding it.
func TestTodo_AGENT_033_TimeoutSweep_Recovery(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	harness := newWorkflowOutcomeHarness(t, now.Add(2*time.Minute))
	ctx := context.Background()
	branch := harness.admit(t, "timeout-sweep", false, now.Add(time.Minute))
	run, err := branch.exec.Start(ctx, branch.record)
	if err != nil {
		t.Fatal(err)
	}
	harness.subscribe(t, branch, run.ID)
	recovery := func() workflowAgentRecovery {
		return workflowAgentRecovery{workflowAgentEvidence: workflowAgentEvidence{tenant: branch.ref, inbox: branch.inbox, state: branch.state}, execution: branch.exec, now: now.Add(2 * time.Minute)}
	}
	delivered, err := branch.bridge.Sweep(ctx, recovery(), branch.ref, 10)
	if err != nil || delivered != 1 {
		t.Fatalf("timeout sweep=%d err=%v", delivered, err)
	}
	result := harness.completionPayload(t, branch, run.ID)
	if result.Outcome != workflowbridge.Expired || result.Code != "EXPIRED" || result.RunID != run.ID || result.OutputRef != "" {
		t.Fatalf("timeout payload=%+v", result)
	}
	if got := harness.continuations(t, branch); got != 1 {
		t.Fatalf("durable continuations=%d want 1", got)
	}
	delivered, err = branch.bridge.Sweep(ctx, recovery(), branch.ref, 10)
	if err != nil || delivered != 0 {
		t.Fatalf("replayed timeout sweep=%d err=%v", delivered, err)
	}
}

// TestTodo_AGENT_033_MissingExecutionSweep_Recovery proves a death between
// admission and execution insertion is repaired by recovery: the sweep
// rebuilds READY execution from the accepted admission without error and
// without inventing a completion.
func TestTodo_AGENT_033_MissingExecutionSweep_Recovery(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	harness := newWorkflowOutcomeHarness(t, now.Add(3*time.Second))
	ctx := context.Background()
	branch := harness.admit(t, "missing-execution", false, now.Add(time.Hour))
	harness.subscribe(t, branch, branch.record.ID)
	recovery := workflowAgentRecovery{workflowAgentEvidence: workflowAgentEvidence{tenant: branch.ref, inbox: branch.inbox, state: branch.state}, execution: branch.exec, now: now.Add(3 * time.Second)}
	delivered, err := branch.bridge.Sweep(ctx, recovery, branch.ref, 10)
	if err != nil || delivered != 0 {
		t.Fatalf("repair sweep=%d err=%v", delivered, err)
	}
	run, err := branch.state.Get(ctx, branch.record.ID)
	if err != nil || run.State != runstate.StateReady || run.AdmissionID != branch.record.ID {
		t.Fatalf("repaired execution=%+v err=%v", run, err)
	}
	if got := harness.continuations(t, branch); got != 0 {
		t.Fatalf("durable continuations=%d want 0", got)
	}
}

// TestTodo_AGENT_033_EarlyCompletion_Integration proves completion ordering
// cannot strand a wait: a terminal run delivered before the SIGNAL node
// subscribes is stored without a wakeup, and the next sweep wakes the late
// subscription exactly once.
func TestTodo_AGENT_033_EarlyCompletion_Integration(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	harness := newWorkflowOutcomeHarness(t, now.Add(3*time.Second))
	ctx := context.Background()
	branch := harness.admit(t, "early-completion", false, now.Add(time.Hour))
	run, err := branch.exec.Start(ctx, branch.record)
	if err != nil {
		t.Fatal(err)
	}
	run, err = branch.exec.Claim(ctx, run.ID, "agent-worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	run, err = branch.exec.Checkpoint(ctx, run.ID, "agent-worker", run.Fence, run.Version, runstate.PhaseValidation, 1, "artifact:typed", branch.record.Request.Agent.Digest, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	run, err = branch.exec.Checkpoint(ctx, run.ID, "agent-worker", run.Fence, run.Version, runstate.PhaseDelivery, 1, "artifact:typed", branch.record.Request.Agent.Digest, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	early, err := branch.bridge.Deliver(ctx, branch.ref, run.ID)
	if err != nil || len(early.Dispositions) != 1 || early.Dispositions[0].Status != stepSignal.StatusRefusedUnmatched {
		t.Fatalf("early completion=%+v err=%v", early, err)
	}
	if got := harness.continuations(t, branch); got != 0 {
		t.Fatalf("early continuations=%d want 0", got)
	}
	harness.subscribe(t, branch, run.ID)
	recovery := workflowAgentRecovery{workflowAgentEvidence: workflowAgentEvidence{tenant: branch.ref, inbox: branch.inbox, state: branch.state}, execution: branch.exec, now: now.Add(3 * time.Second)}
	delivered, err := branch.bridge.Sweep(ctx, recovery, branch.ref, 10)
	if err != nil || delivered != 1 {
		t.Fatalf("late-subscription sweep=%d err=%v", delivered, err)
	}
	result := harness.completionPayload(t, branch, run.ID)
	if result.Outcome != workflowbridge.Succeeded || result.OutputRef != "artifact:typed" || result.OutputDigest != branch.record.Request.Agent.Digest {
		t.Fatalf("late payload=%+v", result)
	}
	if got := harness.continuations(t, branch); got != 1 {
		t.Fatalf("durable continuations=%d want 1", got)
	}
}
