package agentsystem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentauditstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentbudgetstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// TestTodo_AGENT2_026_PostgresComposition runs the same read and model plan
// over the durable PostgreSQL stores under the application role, then
// rebuilds every store from the database and checks nothing was lost: the
// task and its ledger, the grant, and the verified audit chain.
func TestTodo_AGENT2_026_PostgresComposition(t *testing.T) {
	db := pgtest.New(t)
	tenantUUID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantUUID, tenantKey, tenantKey)
	mapper := func(key values.TenantId) uuid.UUID {
		if key == tenantKey {
			return tenantUUID
		}
		return uuid.Nil
	}
	appConn := func() *pgxadapter.Conn {
		conn := db.NewConn(t)
		if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
			t.Fatalf("assume app role: %v", err)
		}
		return conn
	}
	build := func() (*agentdelegationstore.Store, *agentrunstore.Store, *agentauditstore.Store, *agentbudgetstore.Store) {
		grants, err := agentdelegationstore.New(appConn(), mapper)
		if err != nil {
			t.Fatal(err)
		}
		tasks, err := agentrunstore.New(appConn(), mapper)
		if err != nil {
			t.Fatal(err)
		}
		audit, err := agentauditstore.New(appConn(), mapper)
		if err != nil {
			t.Fatal(err)
		}
		budget, err := agentbudgetstore.New(appConn(), mapper)
		if err != nil {
			t.Fatal(err)
		}
		return grants, tasks, audit, budget
	}

	grants, tasks, audit, budget := build()
	f := newFixtureWith(t, fixtureStores{Grants: grants, Tasks: tasks, Audit: audit, Budget: budget})
	f.defaultOwner(t)
	task := f.start(t, "task-pg", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	for range 2 {
		var err error
		if task, err = f.runner.Step(context.Background(), task.ID, ModeOnBehalfOf); err != nil {
			t.Fatalf("Step: %v", err)
		}
	}
	if task.State != agentrun.StateCompleted {
		t.Fatalf("state = %s, want COMPLETED", task.State)
	}

	// A fresh set of stores over new connections is the restart.
	grants2, tasks2, audit2, budget2 := build()
	reloaded, err := tasks2.ForTenant(context.Background(), tenantKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(context.Background(), "task-pg")
	if err != nil || got.State != agentrun.StateCompleted || len(got.Ledger.Entries) != len(task.Ledger.Entries) {
		t.Fatalf("reloaded task = state %s ledger %d err %v, want the completed task and its ledger", got.State, len(got.Ledger.Entries), err)
	}
	scoped, err := grants2.ForTenant(context.Background(), tenantKey)
	if err != nil {
		t.Fatal(err)
	}
	if grant, err := scoped.Get(GrantID("task-pg")); err != nil || grant.UserID != "user-42" {
		t.Fatalf("reloaded grant = %+v, %v", grant, err)
	}
	if err := audit2.Verify(context.Background(), tenantKey); err != nil {
		t.Fatalf("durable audit chain: %v", err)
	}
	views, err := audit2.Query(context.Background(), agentaudit.Query{Viewer: agentaudit.Viewer{TenantID: tenantKey, UserID: "user-42", Role: agentaudit.ViewerUser}, TaskID: "task-pg"})
	if err != nil || len(views) < 4 {
		t.Fatalf("durable audit events = %d, %v, want tool and model events", len(views), err)
	}
	restored, err := agentbudget.NewWithPersistence(testPolicy(), func() time.Time { return fixedNow }, budget2)
	if err != nil {
		t.Fatal(err)
	}
	state, err := budget2.Load(context.Background(), tenantKey, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(state); err != nil {
		t.Fatal(err)
	}
	before, after := f.ledger.Snapshot(), restored.Snapshot()
	if len(after.Tasks) != 1 || after.Tasks[0].Used != before.Tasks[0].Used || after.Tasks[0].Used.Steps < 3 {
		t.Fatalf("restored budget = %+v, want the charged usage %+v", after.Tasks, before.Tasks)
	}
	other, err := tasks2.ForTenant(context.Background(), "other-corp")
	if err == nil {
		if _, getErr := other.Get(context.Background(), "task-pg"); !errors.Is(getErr, agentrun.ErrNotFound) {
			t.Fatalf("another tenant read the durable task: %v", getErr)
		}
	}
}
