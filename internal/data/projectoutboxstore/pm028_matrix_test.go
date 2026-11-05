package projectoutboxstore

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/pressly/goose/v3"
)

func pm028Store(t *testing.T) *projectstore.Store {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(projectstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := projectstore.New(context.Background(), projectstore.Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func pm028SeedTaskFlow(t *testing.T, s *projectstore.Store, tenant, project string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateProject(ctx, projectstore.ProjectRecord{TenantID: tenant, ID: project, OwnerID: "owner", Name: "Board", Timezone: "UTC"}, "owner", "HUMAN", "pm028-create"); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO project_workflow_version(tenant_id,project_id,version,source_revision,config_json,digest,publisher_id) VALUES($1,$2,1,1,'{}'::jsonb,repeat('a',64),'owner')`, tenant, project); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_workflow_current(tenant_id,project_id,version) VALUES($1,$2,1)`, tenant, project)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTaskWithConfig(ctx, projectstore.TaskRecord{ID: "pm028-task", TenantID: tenant, ProjectID: project, Title: "Task", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 1}, 1, "owner", "HUMAN", "pm028-task"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveTask(ctx, tenant, project, "pm028-task", "doing", 1, 1, nil, "owner", "HUMAN", "pm028-move"); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_PM_028 proves the GREEN producer/consumer contract: versioned
// per-project sequences pull in order with event IDs and classification,
// and durable acks advance exactly one cursor step each.
func TestTodo_PM_028(t *testing.T) {
	s := pm028Store(t)
	ctx := context.Background()
	const tenant, project, consumer = "tenant-pm028", "project-pm028", "consumer-1"
	pm028SeedTaskFlow(t, s, tenant, project)
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		batch, err := Pull(ctx, tx, tenant, project, 0, MaxBatchSize)
		if err != nil || len(batch) != 3 {
			t.Fatalf("initial pull = %d events, err=%v", len(batch), err)
		}
		want := []string{"project.created", "task.created", "task.moved"}
		for i, event := range batch {
			if event.Sequence != int64(i+1) || event.Type != want[i] || event.ID == "" || event.SchemaVersion < 1 || event.Classification == "" {
				t.Fatalf("event[%d] = %+v", i, event)
			}
			if err := Ack(ctx, tx, tenant, project, consumer, event); err != nil {
				t.Fatalf("ack %d: %v", event.Sequence, err)
			}
			cursor, err := Cursor(ctx, tx, tenant, project, consumer)
			if err != nil || cursor != int64(i+1) {
				t.Fatalf("cursor after ack %d = %d, err=%v", event.Sequence, cursor, err)
			}
		}
		rest, err := Pull(ctx, tx, tenant, project, 3, MaxBatchSize)
		if err != nil || len(rest) != 0 {
			t.Fatalf("post-cursor pull = %d events, err=%v", len(rest), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_PM_028_Recovery proves crash/replay safety: duplicate acks are
// idempotent, pulls replay only unacked events, and out-of-order or
// mismatched acks fail typed instead of corrupting the cursor.
func TestTodo_PM_028_Recovery(t *testing.T) {
	s := pm028Store(t)
	ctx := context.Background()
	const tenant, project, consumer = "tenant-pm028-recovery", "project-pm028-recovery", "consumer-1"
	pm028SeedTaskFlow(t, s, tenant, project)
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		batch, err := Pull(ctx, tx, tenant, project, 0, MaxBatchSize)
		if err != nil || len(batch) != 3 {
			t.Fatalf("initial pull = %d events, err=%v", len(batch), err)
		}
		if err := Ack(ctx, tx, tenant, project, consumer, batch[0]); err != nil {
			t.Fatal(err)
		}
		// Crash between processing and commit replays from the cursor:
		// the committed event is never redelivered as new work.
		replay, err := Pull(ctx, tx, tenant, project, 1, MaxBatchSize)
		if err != nil || len(replay) != 2 || replay[0].ID == batch[0].ID {
			t.Fatalf("replay after partial ack = %+v, err=%v", replay, err)
		}
		// Duplicate ack after a crash is a safe no-op.
		if err := Ack(ctx, tx, tenant, project, consumer, batch[0]); err != nil {
			t.Fatalf("duplicate ack: %v", err)
		}
		if cursor, err := Cursor(ctx, tx, tenant, project, consumer); err != nil || cursor != 1 {
			t.Fatalf("cursor after duplicate ack = %d, err=%v", cursor, err)
		}
		// Skipping ahead or acking the wrong event ID for a sequence
		// fails typed and leaves the cursor untouched.
		if err := Ack(ctx, tx, tenant, project, consumer, batch[2]); !errors.Is(err, ErrSequenceConflict) {
			t.Fatalf("skipped ack error = %v", err)
		}
		forged := batch[1]
		forged.ID = batch[0].ID
		if err := Ack(ctx, tx, tenant, project, consumer, forged); !errors.Is(err, ErrSequenceConflict) {
			t.Fatalf("forged ack error = %v", err)
		}
		if cursor, err := Cursor(ctx, tx, tenant, project, consumer); err != nil || cursor != 1 {
			t.Fatalf("cursor after rejected acks = %d, err=%v", cursor, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_PM_028_Integration proves consumers hold independent cursors
// over one shared per-project stream with no cross-tenant leakage.
func TestTodo_PM_028_Integration(t *testing.T) {
	s := pm028Store(t)
	ctx := context.Background()
	const project = "project-pm028-integration"
	pm028SeedTaskFlow(t, s, "tenant-a", project)
	pm028SeedTaskFlow(t, s, "tenant-b", project)
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		batch, err := Pull(ctx, tx, "tenant-a", project, 0, MaxBatchSize)
		if err != nil || len(batch) != 3 {
			t.Fatalf("tenant-a pull = %d events, err=%v", len(batch), err)
		}
		for _, event := range batch[:2] {
			if err := Ack(ctx, tx, "tenant-a", project, "fast", event); err != nil {
				t.Fatal(err)
			}
		}
		if err := Ack(ctx, tx, "tenant-a", project, "slow", batch[0]); err != nil {
			t.Fatal(err)
		}
		fast, err := Cursor(ctx, tx, "tenant-a", project, "fast")
		if err != nil || fast != 2 {
			t.Fatalf("fast cursor = %d, err=%v", fast, err)
		}
		slow, err := Cursor(ctx, tx, "tenant-a", project, "slow")
		if err != nil || slow != 1 {
			t.Fatalf("slow cursor = %d, err=%v", slow, err)
		}
		if err := Ack(ctx, tx, "tenant-a", project, "slow", batch[0]); err != nil {
			t.Fatalf("independent consumer replay ack: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		batch, err := Pull(ctx, tx, "tenant-b", project, 0, MaxBatchSize)
		if err != nil || len(batch) != 3 {
			t.Fatalf("tenant-b pull = %d events, err=%v", len(batch), err)
		}
		for _, event := range batch {
			if event.TenantID != "tenant-b" {
				t.Fatalf("tenant leak: %+v", event)
			}
		}
		cursor, err := Cursor(ctx, tx, "tenant-b", project, "fast")
		if err != nil || cursor != 0 {
			t.Fatalf("tenant-b fast cursor = %d, err=%v", cursor, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
