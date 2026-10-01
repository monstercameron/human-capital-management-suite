package agentownerstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }
func TestTodo_AGENT2_022_Integration(t *testing.T) {
	db := pgtest.New(t)
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'owner-one','cell-local','Owner','ACTIVE',now())`, id)
	now := time.Now().UTC().Truncate(time.Microsecond)
	mapper := func(tenant values.TenantId) uuid.UUID {
		if tenant == "owner-one" {
			return id
		}
		return uuid.Nil
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	store, err := New(conn, mapper, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	tasks, _ := store.tasks.Scoped("owner-one")
	runtime, err := agentrun.NewRuntime(tasks)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := agentrun.NewPlan([]agentrun.PlanStep{{ID: "read", Type: agentrun.StepRead, SkillID: "read", SkillVersion: 1, ExpectedOutput: "facts", Tier: agentrun.TierRead}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := runtime.CreateTask(context.Background(), agentrun.CreateRequest{ID: "task", TenantID: "owner-one", UserID: "user", Goal: "private goal", Plan: plan, Now: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.BindTask(context.Background(), "owner-one", "task", "owner", "agent", "1", "install"); err != nil {
		t.Fatal(err)
	}
	if err = store.BindTask(context.Background(), "owner-one", "task", "owner", "agent", "1", "install"); err != nil {
		t.Fatalf("binding retry: %v", err)
	}
	if err = store.BindTask(context.Background(), "owner-one", "task", "other", "agent", "1", "install"); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("binding overwrite: %v", err)
	}
	db.Exec(t, `INSERT INTO agent_budget_task(tenant_id,task_id,tenant_ref,user_id,limit_steps,limit_tokens,limit_wall_ns,limit_spend,used_spend,attempts,failures,created_at,updated_at) VALUES($1,'task','owner-one','user',20,1000,1000000000,42,42,'{}'::jsonb,'{}'::jsonb,$2,$2)`, id, now)
	db.Exec(t, `INSERT INTO agent_budget_event(tenant_id,task_id,kind,step_id,wall_ns,spend,occurred_at) VALUES($1,'task','SETTLE','read',1000,42,$2)`, id, now)
	for _, grant := range []struct{ subject, audience, purpose string }{{"user", "MEMBER", ownerops.PurposeOwnerDashboard}, {"owner", "OWNER", ownerops.PurposeOwnerDashboard}, {"operator", "OPERATOR", ownerops.PurposeOperatorOps}} {
		db.Exec(t, `INSERT INTO agent_owner_grant VALUES($1,$2,$2,$3,$4,ARRAY['agent.operations.read','agent.installation.pause'],'',$5,$6,NULL,'review:1')`, id, grant.subject, grant.audience, grant.purpose, now.Add(-time.Minute), now.Add(time.Hour))
	}
	db.Exec(t, `INSERT INTO agent_owner_grant VALUES($1,'one-task','scoped-operator','OPERATOR',$2,ARRAY['agent.operations.read'],'task',$3,$4,NULL,'review:one-task')`, id, ownerops.PurposeOperatorOps, now.Add(-time.Minute), now.Add(time.Hour))
	ids, err := store.ReadTaskIDs(context.Background(), "owner-one", "scoped-operator", ownerops.AudienceOperator, ownerops.PurposeOperatorOps)
	if err != nil || len(ids) != 1 || ids[0] != "task" {
		t.Fatalf("exact grant inventory: %+v %v", ids, err)
	}
	if _, err = store.ResolveScope(context.Background(), "owner-one", "scoped-operator", ownerops.AudienceOperator, ownerops.PurposeOperatorOps, "different-task"); !errors.Is(err, ownerops.ErrDenied) {
		t.Fatalf("target widening: %v", err)
	}
	scope, err := store.ResolveScope(context.Background(), "owner-one", "owner", ownerops.AudienceOwner, ownerops.PurposeOwnerDashboard, "")
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.Records(context.Background(), "owner-one", "")
	if err != nil {
		t.Fatal(err)
	}
	views, err := ownerops.ProjectTasks(scope, records)
	if err != nil || len(views) != 1 || views[0].Full != nil || views[0].AgentID != "agent" {
		t.Fatalf("owner projection: %+v %v", views, err)
	}
	if views[0].SpendMicros != 42 || !views[0].OverBudget || len(views[0].Steps) != 1 || views[0].Steps[0].Latency != time.Microsecond || views[0].Steps[0].SpendMicros != 42 {
		t.Fatalf("durable measured cost/latency: %+v", views)
	}
	request := ownerops.StopRequest{Kind: ownerops.PauseTask, TenantID: "owner-one", TaskID: task.ID, ExpectedRevision: task.Version, RequestID: "request", IncidentID: "incident", Reason: "investigate run"}
	withoutReason := request
	withoutReason.Reason = " "
	if _, err = ownerops.Stop(context.Background(), scope, withoutReason, store); !errors.Is(err, ownerops.ErrInvalid) {
		t.Fatalf("missing stop reason: %v", err)
	}
	audit, err := ownerops.Stop(context.Background(), scope, request, store)
	if err != nil || audit == "" {
		t.Fatalf("pause: %q %v", audit, err)
	}
	paused, err := tasks.Get(context.Background(), task.ID)
	if err != nil || paused.State != agentrun.StatePaused || paused.Version != task.Version+1 || paused.WorkerLease != "" {
		t.Fatalf("durable fence: %+v %v", paused, err)
	}
	retry, err := ownerops.Stop(context.Background(), scope, request, store)
	if err != nil || retry != audit {
		t.Fatalf("retry: %q %v", retry, err)
	}
	request.IncidentID = "different"
	if _, err = ownerops.Stop(context.Background(), scope, request, store); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("replay key: %v", err)
	}
	request.RequestID = "stale"
	if _, err = ownerops.Stop(context.Background(), scope, request, store); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	db.Exec(t, `UPDATE agent_owner_grant SET revoked_at=$2 WHERE tenant_id=$1 AND principal_id='owner'`, id, now)
	request.ExpectedRevision = paused.Version
	if _, err = ownerops.Stop(context.Background(), scope, request, store); !errors.Is(err, ownerops.ErrDenied) {
		t.Fatalf("cached grant bypass: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if _, err = store.ResolveScope(context.Background(), "owner-one", "operator", ownerops.AudienceOperator, ownerops.PurposeOperatorOps, ""); !errors.Is(err, ownerops.ErrDenied) {
		t.Fatalf("expired operator grant: %v", err)
	}
	if _, err = store.ResolveScope(context.Background(), "foreign", "operator", ownerops.AudienceOperator, ownerops.PurposeOperatorOps, ""); !errors.Is(err, ownerops.ErrDenied) {
		t.Fatalf("cross tenant: %v", err)
	}
}
func TestTodo_AGENT_041_Security(t *testing.T) {
	if _, err := New(nil, nil, nil); !errors.Is(err, ownerops.ErrInvalid) {
		t.Fatalf("missing runtime: %v", err)
	}
	var store *Store
	if _, err := store.ResolveScope(context.Background(), "tenant", "user", ownerops.AudienceOwner, ownerops.PurposeOwnerDashboard, ""); !errors.Is(err, ownerops.ErrDenied) {
		t.Fatalf("nil store: %v", err)
	}
}
