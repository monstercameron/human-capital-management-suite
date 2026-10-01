package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT_041_Integration(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	ctx := context.Background()
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	now := time.Now().UTC()
	operations, err := NewAgentOwnerOperations(f.pool, mapper, f.cell.RoleAccess, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	p := agentTestPrincipal(t, f.tenant, agentTestWorker)
	if _, err = operations.Dashboard(ctx, p, ownerops.AudienceMember); !errors.Is(err, ownerops.ErrDenied) {
		t.Fatalf("no explicit grant: %v", err)
	}
	task, err := f.runtime.Starter.StartTaskMode(ctx, p, "private goal", agentclient.StartLongTask)
	if err != nil {
		t.Fatal(err)
	}
	id := mapper(values.TenantId(f.tenant))
	f.db.Exec(t, `INSERT INTO agent_owner_grant VALUES($1,'member',$2,'MEMBER',$3,ARRAY['agent.operations.read','agent.installation.pause'],$6,$4,$5,NULL,'review:member')`, id, p.Subject(), ownerops.PurposeOwnerDashboard, now.Add(-time.Minute), now.Add(time.Hour), task.ID)
	views, err := operations.Dashboard(ctx, p, ownerops.AudienceMember)
	if err != nil || len(views) != 1 || views[0].Full == nil || views[0].Full.Goal != "private goal" || !views[0].CanPause {
		t.Fatalf("own durable view: %+v %v", views, err)
	}
	_, err = operations.Stop(ctx, p, ownerops.AudienceMember, ownerops.StopRequest{Kind: ownerops.PauseTask, TaskID: task.ID, ExpectedRevision: task.Version, RequestID: "pause", IncidentID: "incident", Reason: "investigate"})
	if err != nil {
		t.Fatal(err)
	}
	exerciseAgentMemoryBoundary(t, f, p, task.ID, now)
	role, err := f.cell.RoleAccess.SaveRole(ctx, values.TenantId(f.tenant), "system:test", roleaccess.Role{ID: "revoked_test", Name: "Revoked test", Active: true, Reason: "test role authority"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.cell.RoleAccess.Load(ctx, values.TenantId(f.tenant), agentOrgScope(values.TenantId(f.tenant)))
	if err != nil {
		t.Fatal(err)
	}
	for _, assignment := range snapshot.Assignments {
		if assignment.WorkerRef == p.Subject() {
			assignment.RoleIDs = []string{role.ID}
			assignment.Reason = "narrow test authority"
			if _, err = f.cell.RoleAccess.SaveAssignment(ctx, values.TenantId(f.tenant), "system:test", assignment); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	role.Active = false
	role.Reason = "revoke test authority"
	if _, err = f.cell.RoleAccess.SaveRole(ctx, values.TenantId(f.tenant), "system:test", role); err != nil {
		t.Fatal(err)
	}
	if _, err = operations.Dashboard(ctx, p, ownerops.AudienceMember); !errors.Is(err, ownerops.ErrDenied) {
		t.Fatalf("cached session after role revocation: %v", err)
	}
}
