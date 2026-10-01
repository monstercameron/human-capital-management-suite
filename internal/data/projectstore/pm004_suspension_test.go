package projectstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

func TestTodo_PM_004_Integration(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	p := ProjectRecord{ID: "pm004-project", TenantID: "tenant-a", OwnerID: "owner-a", Name: "Operations", Timezone: "UTC", Lifecycle: string(project.LifecycleActive), Revision: 1}
	if err := s.CreateProject(ctx, p, "owner-a", "HUMAN", "pm004-create"); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project_membership(tenant_id,project_id,user_id,role,state,revision) VALUES('tenant-a','pm004-project','operator-a','MANAGER','ACTIVE',1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	suspended, err := s.SuspendProject(ctx, "tenant-a", p.ID, 1, "operator-a", "HUMAN", "records incident", "pm004-suspend")
	if err != nil || suspended.Lifecycle != string(project.LifecycleSuspended) || suspended.Revision != 2 {
		t.Fatalf("suspend result = %+v err=%v", suspended, err)
	}
	replayed, err := s.SuspendProject(ctx, "tenant-a", p.ID, 1, "operator-a", "HUMAN", "records incident", "pm004-suspend")
	if err != nil || replayed.Revision != 2 {
		t.Fatalf("suspend replay = %+v err=%v", replayed, err)
	}
	if _, err := s.SuspendProject(ctx, "tenant-a", p.ID, 1, "operator-a", "HUMAN", "different reason", "pm004-suspend"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed suspension replay error = %v", err)
	}
	if err := s.CreateTask(ctx, TaskRecord{ID: "pm004-task", TenantID: "tenant-a", ProjectID: p.ID, Title: "blocked", StatusID: "todo"}, "operator-a", "HUMAN", "pm004-task"); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("suspended project admitted task write: %v", err)
	}
	read, err := s.GetProject(ctx, "tenant-a", p.ID)
	if err != nil || read.Lifecycle != string(project.LifecycleSuspended) {
		t.Fatalf("suspended project read = %+v err=%v", read, err)
	}
	var eventType, reason string
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT event_type,payload->>'reason' FROM project_activity WHERE tenant_id='tenant-a' AND project_id=$1 AND event_type='project.suspended'`, p.ID).Scan(&eventType, &reason)
	}); err != nil {
		t.Fatal(err)
	}
	if eventType != "project.suspended" || reason != "records incident" {
		t.Fatalf("suspension evidence = %q %q", eventType, reason)
	}
}

func TestTodo_PM_004_Security(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	p := ProjectRecord{ID: "pm004-security", TenantID: "tenant-a", OwnerID: "owner-a", Name: "Operations", Timezone: "UTC", Lifecycle: string(project.LifecycleActive), Revision: 1}
	if err := s.CreateProject(ctx, p, "owner-a", "HUMAN", "pm004-security-create"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SuspendProject(ctx, "tenant-a", p.ID, 1, "owner-a", "HUMAN", "incident", "pm004-owner"); !errors.Is(err, project.ErrNotAuthorized) {
		t.Fatalf("owner suspension error = %v", err)
	}
	if _, err := s.SuspendProject(ctx, "tenant-b", p.ID, 1, "operator-a", "HUMAN", "incident", "pm004-cross-tenant"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("cross-tenant suspension error = %v", err)
	}
}
