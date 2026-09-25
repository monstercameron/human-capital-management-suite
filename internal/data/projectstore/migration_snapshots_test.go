package projectstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

func TestTodo_PM_003_ActiveTaskSnapshotTenantIsolation(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if err := s.CreateProject(ctx, ProjectRecord{ID: "same-project", TenantID: tenant, OwnerID: "owner", Name: "Project", Timezone: "UTC", Lifecycle: "ACTIVE"}, "owner", "HUMAN", "create-"+tenant); err != nil {
			t.Fatal(err)
		}
		if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,type_id,priority,fields_json) VALUES($1,$2,'same-project','Task','todo','ops','NORMAL','{"safe_field":"value"}'::jsonb)`, tenant, tenant+"-task")
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.ActiveTaskSnapshots(ctx, "tenant-a", "same-project")
	if err != nil || len(a) != 1 || a[0].ID != "tenant-a-task" || a[0].TypeID != "ops" || a[0].StatusID != "todo" {
		t.Fatalf("tenant-a snapshot: %#v err=%v", a, err)
	}
	if got := string(a[0].Fields["safe_field"]); got != `"value"` {
		t.Fatalf("field value=%s", got)
	}
	b, err := s.ActiveTaskSnapshots(ctx, "tenant-b", "same-project")
	if err != nil || len(b) != 1 || b[0].ID != "tenant-b-task" {
		t.Fatalf("tenant-b snapshot: %#v err=%v", b, err)
	}
	if _, err := s.ActiveTaskSnapshots(ctx, "tenant-a", "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant/missing project snapshot err=%v", err)
	}
}

func TestTodo_PM_003_ActiveTaskSnapshotBoundaryAndOverflow(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	if err := s.CreateProject(ctx, ProjectRecord{ID: "bounded", TenantID: "tenant-a", OwnerID: "owner", Name: "Bounded", Timezone: "UTC", Lifecycle: "ACTIVE"}, "owner", "HUMAN", "create-bounded"); err != nil {
		t.Fatal(err)
	}
	seed := func(start, end int) error {
		return s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,type_id,priority,fields_json)
				SELECT 'tenant-a','task-'||lpad(g::text,5,'0'),'bounded','Task','todo','ops','NORMAL',jsonb_build_object('field_a',to_jsonb(g::text)) FROM generate_series($1::int,$2::int) g`, start, end)
			return err
		})
	}
	if err := seed(1, projectworkflow.MaxTaskSnapshotsPerPreview); err != nil {
		t.Fatal(err)
	}
	snapshots, err := s.ActiveTaskSnapshots(ctx, "tenant-a", "bounded")
	if err != nil || len(snapshots) != projectworkflow.MaxTaskSnapshotsPerPreview {
		t.Fatalf("at-cap snapshot length=%d err=%v", len(snapshots), err)
	}
	if snapshots[0].ID != "task-00001" || snapshots[len(snapshots)-1].ID != "task-10000" {
		t.Fatalf("snapshot order boundary: first=%q last=%q", snapshots[0].ID, snapshots[len(snapshots)-1].ID)
	}
	if got, ok := snapshots[0].Fields["field_a"]; !ok || string(got) != `"1"` {
		t.Fatalf("custom field lost: %#v", snapshots[0].Fields)
	}
	if err := seed(projectworkflow.MaxTaskSnapshotsPerPreview+1, projectworkflow.MaxTaskSnapshotsPerPreview+1); err != nil {
		t.Fatal(err)
	}
	got, err := s.ActiveTaskSnapshots(ctx, "tenant-a", "bounded")
	if !errors.Is(err, projectworkflow.ErrSnapshotLimitExceeded) || got != nil {
		t.Fatalf("overflow returned %d tasks, err=%v; want no partial result and limit error", len(got), err)
	}
}
