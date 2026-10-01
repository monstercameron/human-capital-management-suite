package application

import (
	"context"
	"errors"
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

type workflowAgentTenantRunner struct{ conn *pgxadapter.Conn }

func (r workflowAgentTenantRunner) RunTenantTx(ctx context.Context, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	tx, err := r.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type workflowAgentEvidence struct {
	tenant string
	inbox  *agentrunstore.AdmissionRepository
	state  *agentrunstate.TenantStore
}

type workflowAgentRecovery struct {
	workflowAgentEvidence
	execution *runstate.Service
	now       time.Time
}

func (e workflowAgentRecovery) Recover(ctx context.Context, tenant, id string) (runstate.Run, error) {
	run, err := e.GetRun(ctx, tenant, id)
	if err != nil {
		return runstate.Run{}, err
	}
	return e.execution.Recover(ctx, id, run.Version, e.now)
}

func (e workflowAgentEvidence) GetAdmission(ctx context.Context, tenant, id string) (agentrun.Record, error) {
	if tenant != e.tenant {
		return agentrun.Record{}, workflowbridge.ErrEvidence
	}
	return e.inbox.GetByID(ctx, id)
}
func (e workflowAgentEvidence) GetRun(ctx context.Context, tenant, id string) (runstate.Run, error) {
	if tenant != e.tenant {
		return runstate.Run{}, workflowbridge.ErrEvidence
	}
	return e.state.Get(ctx, id)
}

type workflowAgentRecheck struct{}

func (workflowAgentRecheck) Recheck(context.Context, string, string) error { return nil }

func TestTodo_AGENT_033_Integration(t *testing.T) { testWorkflowAgentDurableCompletion(t, false) }
func TestTodo_AGENT_033_Recovery(t *testing.T)    { testWorkflowAgentDurableCompletion(t, true) }

func testWorkflowAgentDurableCompletion(t *testing.T, restart bool) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	core := pgtest.New(t)
	agents := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agents.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	instance := uuid.New()
	tenantRef := "common-tenant"
	core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,$2,'cell-local','Workflow agents','ACTIVE',$3)`, tenant, tenantRef, now.Add(-time.Hour))
	agents.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant)
	core.Exec(t, `INSERT INTO workflow_instance(tenant_id,instance_id,cell_id,workflow_id,workflow_version,compiled_plan_hash,business_subject_refs,execution_mode,runtime_status,completion_dimensions,input_ref,variable_revision_head,current_node_ids,correlation_id,created_at) VALUES($1,$2,'cell-local','workflow-agent-test',1,repeat('a',64),'{}','EXECUTE','WAITING','{}','input:one',0,'{await_agent}','workflow-agent-test',$3)`, tenant, instance, now)
	coreConn := core.NewConn(t)
	if _, err := coreConn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	agentConn := agents.NewConn(t)
	if _, err := agentConn.Exec(ctx, "SET ROLE hcm_agent_app"); err != nil {
		t.Fatal(err)
	}
	runner := workflowAgentTenantRunner{conn: agentConn}
	inbox, err := agentrunstore.NewAdmissionRepository(runner, tenant, values.TenantId(tenantRef))
	if err != nil {
		t.Fatal(err)
	}
	mapper := func(ref string) uuid.UUID {
		if ref == tenantRef {
			return tenant
		}
		return uuid.Nil
	}
	states, err := agentrunstate.New(runner, mapper)
	if err != nil {
		t.Fatal(err)
	}
	state, err := states.ForTenant(tenantRef)
	if err != nil {
		t.Fatal(err)
	}
	request := commonAgentTestRequest(now)
	request.Source = agentrun.SourceIdentity{TenantID: tenantRef, Kind: agentrun.SourceWorkflow, Ref: "workflow:" + instance.String() + ":analyze"}
	request.Source, err = (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Store: inbox, Authority: &commonAgentTestAuthority{}, Now: func() time.Time { return now }})
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
	run, err := execution.Start(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	run, err = execution.Claim(ctx, run.ID, "agent-worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	bridge := AgentWorkflowCompletionBridge{Evidence: workflowAgentEvidence{tenant: tenantRef, inbox: inbox, state: state}, Current: workflowAgentRecheck{}, Core: coreConn, ResolveTenant: mapper, Now: func() time.Time { return now.Add(3 * time.Second) }}
	if _, err = bridge.Deliver(ctx, tenantRef, run.ID); !errors.Is(err, workflowbridge.ErrPending) {
		t.Fatalf("nonterminal completion=%v", err)
	}
	subscription := uuid.New()
	coreRunner := workflowAgentTenantRunner{conn: coreConn}
	if err = coreRunner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return (signals.Store{}).Subscribe(ctx, tx, signals.Subscription{TenantID: tenant, SubscriptionID: subscription, InstanceID: instance, NodeID: "await_agent", NodeAttempt: 1, EventType: workflowbridge.EventType, CorrelationKey: workflowbridge.CorrelationKey, CorrelationValue: run.ID, ExpectedSchemaRef: workflowbridge.SchemaRef, AcceptedSources: []string{workflowbridge.Source}, Ordering: stepSignal.OrderingNone, CreatedAt: now, ClosesAt: now.Add(time.Minute)})
	}); err != nil {
		t.Fatal(err)
	}
	run, err = execution.Checkpoint(ctx, run.ID, "agent-worker", run.Fence, run.Version, runstate.PhaseValidation, 1, "artifact:typed", request.Agent.Digest, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	run, err = execution.Checkpoint(ctx, run.ID, "agent-worker", run.Fence, run.Version, runstate.PhaseDelivery, 1, "artifact:typed", request.Agent.Digest, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if restart {
		// Recreate both agent repositories and the bridge after the terminal
		// write. No process-local output or pending delivery list survives.
		inbox, err = agentrunstore.NewAdmissionRepository(runner, tenant, values.TenantId(tenantRef))
		if err != nil {
			t.Fatal(err)
		}
		states, err = agentrunstate.New(runner, mapper)
		if err != nil {
			t.Fatal(err)
		}
		state, err = states.ForTenant(tenantRef)
		if err != nil {
			t.Fatal(err)
		}
		bridge.Evidence = workflowAgentEvidence{tenant: tenantRef, inbox: inbox, state: state}
		// Recovery discovers the persisted open correlation and completion;
		// it does not depend on the worker retaining a callback in memory.
		count, sweepErr := bridge.Sweep(ctx, workflowAgentRecovery{workflowAgentEvidence: workflowAgentEvidence{tenant: tenantRef, inbox: inbox, state: state}, execution: execution, now: now.Add(3 * time.Second)}, tenantRef, 1)
		if sweepErr != nil || count != 1 {
			t.Fatalf("recovery sweep=%d err=%v", count, sweepErr)
		}
		count, sweepErr = bridge.Sweep(ctx, workflowAgentRecovery{workflowAgentEvidence: workflowAgentEvidence{tenant: tenantRef, inbox: inbox, state: state}, execution: execution, now: now.Add(3 * time.Second)}, tenantRef, 1)
		if sweepErr != nil || count != 0 {
			t.Fatalf("replayed recovery sweep=%d err=%v", count, sweepErr)
		}
	}
	first, err := bridge.Deliver(ctx, tenantRef, run.ID)
	wantStatus := stepSignal.StatusAccepted
	if restart {
		wantStatus = stepSignal.StatusDuplicateSameBytes
	}
	if err != nil || len(first.Dispositions) != 1 || first.Dispositions[0].Status != wantStatus {
		t.Fatalf("completion=%+v err=%v", first, err)
	}
	second, err := bridge.Deliver(ctx, tenantRef, run.ID)
	if err != nil || second.SignalID != first.SignalID || len(second.Dispositions) != 1 || second.Dispositions[0].Status != stepSignal.StatusDuplicateSameBytes {
		t.Fatalf("duplicate=%+v err=%v", second, err)
	}
	var count int
	if err = core.QueryRow(ctx, `SELECT count(*) FROM workflow_ready_work WHERE tenant_id=$1 AND instance_id=$2`, tenant, instance).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("durable continuations=%d want1", count)
	}
	if _, err = bridge.Deliver(ctx, "other-tenant", run.ID); !errors.Is(err, ErrAgentWorkflowInput) {
		t.Fatalf("cross tenant=%v", err)
	}
}

func TestTodo_AGENT_033_CompletionFault(t *testing.T) {
	if _, err := (AgentWorkflowCompletionBridge{}).Deliver(context.Background(), "tenant", "run"); !errors.Is(err, ErrAgentWorkflowInput) {
		t.Fatalf("missing completion ports=%v", err)
	}
	signal := stepSignal.Signal{Source: workflowbridge.Source, EventType: workflowbridge.EventType, Payload: []byte(`{"outcome":"SUCCEEDED"}`)}
	verifier := agentWorkflowSignalVerifier{expected: signal}
	if err := verifier.Verify(signal); err != nil {
		t.Fatal(err)
	}
	signal.CorrelationValue = "foreign-run"
	if err := verifier.Verify(signal); err == nil {
		t.Fatal("forged correlation accepted")
	}
}
