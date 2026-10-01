package agentsystem

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentauditstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentbudgetstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_AGENT_050_Integration reconstructs the runtime from fresh database
// connections before running the child. Grant ancestry, task ancestry, shared
// accounting and later parent cancellation must all survive the restart.
func TestTodo_AGENT_050_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, tenantKey, tenantKey)
	mapper := func(key values.TenantId) uuid.UUID {
		if key == tenantKey {
			return tenantID
		}
		return uuid.Nil
	}
	build := func() fixtureStores {
		conn := db.NewConn(t)
		if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
			t.Fatal(err)
		}
		grants, err := agentdelegationstore.New(conn, mapper)
		if err != nil {
			t.Fatal(err)
		}
		tasks, err := agentrunstore.New(conn, mapper)
		if err != nil {
			t.Fatal(err)
		}
		audit, err := agentauditstore.New(conn, mapper)
		if err != nil {
			t.Fatal(err)
		}
		budget, err := agentbudgetstore.New(conn, mapper)
		if err != nil {
			t.Fatal(err)
		}
		return fixtureStores{Grants: grants, Tasks: tasks, Audit: audit, Budget: budget}
	}
	first := newFixtureWith(t, build())
	first.defaultOwner(t)
	parent := first.start(t, "parent-pg", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	child, err := first.runner.Delegate(ctx, specialistRequest(t, first, parent, "child-pg"))
	if err != nil {
		t.Fatal(err)
	}
	freshStores := build()
	restarted := newFixtureWith(t, freshStores)
	restarted.defaultOwner(t)
	persistence := freshStores.Budget.(*agentbudgetstore.Store)
	state, err := persistence.Load(ctx, tenantKey, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.ledger.Restore(state); err != nil {
		t.Fatal(err)
	}
	stored, err := restarted.runner.Runtime.GetTask(ctx, child.ID)
	if err != nil || stored.ParentTaskID != parent.ID || stored.RootTaskID != parent.ID {
		t.Fatalf("reloaded child = %+v, %v", stored, err)
	}
	grant, err := restarted.runner.grants.Get(GrantID(child.ID))
	if err != nil || grant.ParentActor == nil || grant.ParentGrantID != GrantID(parent.ID) {
		t.Fatalf("reloaded child grant = %+v, %v", grant, err)
	}
	done, err := restarted.runner.Step(ctx, child.ID, ModeOnBehalfOf)
	if err != nil || done.State != agentrun.StateCompleted {
		t.Fatalf("restarted child = %s, %v", done.State, err)
	}
	charged := restarted.ledger.Snapshot()
	var rootSteps, childSteps int64
	for _, task := range charged.Tasks {
		if task.ID == parent.ID {
			rootSteps = task.Used.Steps
		}
		if task.ID == child.ID {
			childSteps = task.Used.Steps
		}
	}
	if rootSteps == 0 || rootSteps != childSteps {
		t.Fatalf("shared budget root=%d child=%d", rootSteps, childSteps)
	}
	// Parent cancellation is read from a fresh connection by a second child.
	other, err := restarted.runner.Delegate(ctx, specialistRequest(t, restarted, parent, "child-stop"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.runner.Runtime.Cancel(ctx, parent.ID, parent.Version, fixedNow); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.runner.Step(ctx, other.ID, ModeOnBehalfOf); !errors.Is(err, ErrParentStopped) {
		t.Fatalf("durable parent stop = %v", err)
	}
	if err := restarted.runner.Delegation.RevokeGrant(GrantID(parent.ID), "suspended"); err != nil {
		t.Fatal(err)
	}
	credentialReq := agentdelegation.ExchangeRequest{SubjectToken: GrantID(child.ID), SubjectTokenType: agentdelegation.DelegationGrantTokenType, RunID: child.ID, StepID: "analyze", Skill: "skill.summarize", Scope: grant.SkillScopes["skill.summarize"], Audience: restarted.platform.cfg.Audience, Sender: restarted.platform.cfg.Workload}
	if _, err := restarted.runner.Delegation.Exchange(credentialReq); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("durable ancestor revoke = %v", err)
	}
}
