package projectstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

func TestTodo_PM_008_Integration(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	p := ProjectRecord{ID: "lifecycle-project", TenantID: "tenant-a", OwnerID: "owner-a", Name: "Launch", Timezone: "UTC", Lifecycle: string(project.LifecycleActive), Revision: 1}
	if err := s.CreateProject(ctx, p, "owner-a", "HUMAN", "create-project"); err != nil {
		t.Fatal(err)
	}

	updated, err := s.UpdateProjectSettings(ctx, "tenant-a", p.ID, "Launch 2", "Europe/Paris", 1, "manager-a", "HUMAN", "settings-1")
	if err != nil || updated.Name != "Launch 2" || updated.Timezone != "Europe/Paris" || updated.Revision != 2 {
		t.Fatalf("settings update = %+v err=%v", updated, err)
	}
	replayed, err := s.UpdateProjectSettings(ctx, "tenant-a", p.ID, "Launch 2", "Europe/Paris", 1, "manager-a", "HUMAN", "settings-1")
	if err != nil || replayed.Revision != 2 {
		t.Fatalf("settings replay = %+v err=%v", replayed, err)
	}
	if _, err := s.UpdateProjectSettings(ctx, "tenant-a", p.ID, "Other", "UTC", 1, "manager-a", "HUMAN", "settings-1"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed settings replay err=%v", err)
	}
	if _, err := s.UpdateProjectSettings(ctx, "tenant-b", p.ID, "Cross tenant", "UTC", 2, "manager-a", "HUMAN", "cross-tenant"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant settings update err=%v", err)
	}
	if _, err := s.UpdateProjectSettings(ctx, "tenant-a", p.ID, "Stale", "UTC", 1, "manager-a", "HUMAN", "stale-settings"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale settings revision err=%v", err)
	}
	if _, err := s.TransitionProject(ctx, "tenant-a", p.ID, project.LifecycleArchived, 2, "manager-a", "HUMAN", "not-owner"); !errors.Is(err, project.ErrNotAuthorized) {
		t.Fatalf("non-owner lifecycle mutation err=%v", err)
	}
	archived, err := s.TransitionProject(ctx, "tenant-a", p.ID, project.LifecycleArchived, 2, "owner-a", "HUMAN", "archive-1")
	if err != nil || archived.Lifecycle != string(project.LifecycleArchived) || archived.Revision != 3 {
		t.Fatalf("archive result = %+v err=%v", archived, err)
	}
	archivedReplay, err := s.TransitionProject(ctx, "tenant-a", p.ID, project.LifecycleArchived, 2, "owner-a", "HUMAN", "archive-1")
	if err != nil || archivedReplay.Revision != 3 {
		t.Fatalf("archive replay = %+v err=%v", archivedReplay, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE project SET owner_id='owner-b' WHERE tenant_id='tenant-a' AND id=$1`, p.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransitionProject(ctx, "tenant-a", p.ID, project.LifecycleArchived, 2, "owner-a", "HUMAN", "archive-1"); !errors.Is(err, project.ErrNotAuthorized) {
		t.Fatalf("former owner replay err=%v", err)
	}
	restored, err := s.TransitionProject(ctx, "tenant-a", p.ID, project.LifecycleActive, 3, "owner-b", "HUMAN", "restore-1")
	if err != nil || restored.Lifecycle != string(project.LifecycleActive) || restored.Revision != 4 {
		t.Fatalf("restore result = %+v err=%v", restored, err)
	}
	if _, err := s.TransitionProject(ctx, "tenant-a", p.ID, project.LifecycleArchived, 3, "owner-b", "HUMAN", "stale-archive"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale lifecycle revision err=%v", err)
	}
	var activityCount, outboxCount int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2`, "tenant-a", p.ID).Scan(&activityCount); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_outbox WHERE tenant_id=$1 AND project_id=$2`, "tenant-a", p.ID).Scan(&outboxCount)
	}); err != nil {
		t.Fatal(err)
	}
	if activityCount != 4 || outboxCount != 4 {
		t.Fatalf("project commands emitted activity=%d outbox=%d; want 4 each", activityCount, outboxCount)
	}
}
