package agentownerstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_AGENT_041_IncidentCorrelation: a task paused under an incident
// keeps that correlation on its durable record; a task with no stop evidence
// carries none, and one task's incident never attaches to another.
func TestTodo_AGENT_041_IncidentCorrelation(t *testing.T) {
	db := pgtest.New(t)
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'owner-incident','cell-local','Owner','ACTIVE',now())`, id)
	now := time.Now().UTC().Truncate(time.Microsecond)
	mapper := func(tenant values.TenantId) uuid.UUID {
		if tenant == "owner-incident" {
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
	tasks, _ := store.tasks.Scoped("owner-incident")
	runtime, err := agentrun.NewRuntime(tasks)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := agentrun.NewPlan([]agentrun.PlanStep{{ID: "read", Type: agentrun.StepRead, SkillID: "read", SkillVersion: 1, ExpectedOutput: "facts", Tier: agentrun.TierRead}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	paused, err := runtime.CreateTask(ctx, agentrun.CreateRequest{ID: "task-paused", TenantID: "owner-incident", UserID: "user", Goal: "private goal", Plan: plan, Now: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.CreateTask(ctx, agentrun.CreateRequest{ID: "task-clean", TenantID: "owner-incident", UserID: "user", Goal: "private goal", Plan: plan, Now: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, taskID := range []string{"task-paused", "task-clean"} {
		if err = store.BindTask(ctx, "owner-incident", taskID, "owner", "agent", "1", "install"); err != nil {
			t.Fatal(err)
		}
	}
	db.Exec(t, `INSERT INTO agent_owner_grant VALUES($1,'owner','owner','OWNER',$2,ARRAY['agent.operations.read','agent.installation.pause'],'',$3,$4,NULL,'review:1')`, id, ownerops.PurposeOwnerDashboard, now.Add(-time.Minute), now.Add(time.Hour))
	scope, err := store.ResolveScope(ctx, "owner-incident", "owner", ownerops.AudienceOwner, ownerops.PurposeOwnerDashboard, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ownerops.Stop(ctx, scope, ownerops.StopRequest{Kind: ownerops.PauseTask, TenantID: "owner-incident", TaskID: paused.ID, ExpectedRevision: paused.Version, RequestID: "request-incident", IncidentID: "INC-1042", Reason: "investigate run"}, store); err != nil {
		t.Fatal(err)
	}
	records, err := store.Records(ctx, "owner-incident", "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ownerops.TaskRecord{}
	for _, record := range records {
		byID[record.Task.ID] = record
	}
	if byID["task-paused"].IncidentID != "INC-1042" {
		t.Fatalf("paused task incident = %q, want INC-1042", byID["task-paused"].IncidentID)
	}
	if byID["task-clean"].IncidentID != "" {
		t.Fatalf("clean task incident = %q, want empty", byID["task-clean"].IncidentID)
	}
	views, err := ownerops.ProjectTasks(scope, records)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.TaskID == "task-paused" && view.IncidentID != "INC-1042" {
			t.Fatalf("paused task view incident = %q, want INC-1042", view.IncidentID)
		}
	}
}
