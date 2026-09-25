package projectstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	projectdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func projectFixture(t *testing.T) (*Store, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Config{DSN: db.URL, CoreDSN: "postgres://other:secret@127.0.0.1:5432/postgres?sslmode=disable", Schema: db.Schema, MaxConns: 4, MinConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, db.Schema
}

func TestTodo_PM_003(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("empty project DSN accepted")
	}
	if _, err := New(context.Background(), Config{DSN: "postgres://u:p@host:5432/core", Schema: "bad-name"}); err == nil {
		t.Fatal("invalid project schema accepted")
	}
	if _, err := withProjectPoolSize("postgres://u:p@host:5432/core", 2, 9); err == nil {
		t.Fatal("inverted pool bounds accepted")
	}
	got, err := withProjectPoolSize("host=db port=5432 dbname=core", 8, 2)
	if err != nil || got != "host=db port=5432 dbname=core pool_max_conns=8 pool_min_conns=2" {
		t.Fatalf("pool bounds: %q err=%v", got, err)
	}
	got, err = withProjectPoolSize("postgres://u:p@host:5432/core?pool_max_conns=2", 8, 1)
	if err != nil || got != "postgres://u:p@host:5432/core?pool_max_conns=2&pool_min_conns=1" {
		t.Fatalf("operator pool bound overwritten: %q err=%v", got, err)
	}
	if _, err := withSchema("not a dsn", "projects"); err == nil {
		t.Fatal("invalid DSN accepted")
	}
	if got, err = withSchema("postgres://u:p@host:5432/core", "projects"); err != nil || !strings.Contains(got, "search_path=projects") {
		t.Fatalf("schema search path: %q err=%v", got, err)
	}
	if sameDatabase("postgres://u:p@db:5432/core", "postgres://x:y@db:5432/core") != true {
		t.Fatal("logical database comparison lost same-database result")
	}
	if _, err := New(context.Background(), Config{DSN: "postgres://u:p@db:5432/core", CoreDSN: "postgres://u:p@db:5432/core", Schema: "projects"}); err != ErrCoreCredential {
		t.Fatalf("shared credential error=%v", err)
	}
}

func TestTodo_PM_003_Integration(t *testing.T) {
	s, schema := projectFixture(t)
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES($1,$2,'owner','Project','UTC')`, tenant, tenant+"-project")
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	project := ProjectRecord{ID: "project-new", TenantID: "tenant-a", OwnerID: "owner", Name: "New Project", Timezone: "UTC", Lifecycle: "ACTIVE", Revision: 1}
	if err := s.CreateProject(ctx, project, "owner", "HUMAN", "create-project-1"); err != nil {
		t.Fatal(err)
	}
	seedCurrentWorkflow(t, s, "tenant-a", "project-new", "owner", 1)
	if err := s.CreateProject(ctx, project, "owner", "HUMAN", "create-project-1"); err != nil {
		t.Fatalf("create replay: %v", err)
	}
	changed := project
	changed.Name = "Changed"
	if err := s.CreateProject(ctx, changed, "owner", "HUMAN", "create-project-1"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay err=%v", err)
	}
	if got, err := s.GetProject(ctx, "tenant-a", "project-new"); err != nil || got.Name != "New Project" || got.Revision != 1 {
		t.Fatalf("get project: %#v err=%v", got, err)
	}
	projects, err := s.ListProjects(ctx, "tenant-a", "", 10)
	if err != nil || len(projects) != 2 {
		t.Fatalf("list projects: %#v err=%v", projects, err)
	}
	initFailure := errors.New("starter configuration rejected")
	failedProject := ProjectRecord{ID: "project-rollback", TenantID: "tenant-a", OwnerID: "owner", Name: "Rollback", Timezone: "UTC", Lifecycle: "ACTIVE", Revision: 1}
	if err := s.CreateProjectWith(ctx, failedProject, "owner", "HUMAN", "create-rollback", func(context.Context, dbport.Tx) error { return initFailure }); !errors.Is(err, initFailure) {
		t.Fatalf("initializer error=%v", err)
	}
	if _, err := s.GetProject(ctx, "tenant-a", failedProject.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("project survived initializer rollback: %v", err)
	}
	task := TaskRecord{ID: "task-new", TenantID: "tenant-a", ProjectID: "project-new", Title: "First task", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 1}
	if err := s.CreateTaskWithConfig(ctx, task, 1, "owner", "HUMAN", "create-task-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTaskWithConfig(ctx, task, 1, "owner", "HUMAN", "create-task-1"); err != nil {
		t.Fatalf("task replay: %v", err)
	}
	if err := s.CreateTaskWithConfig(ctx, task, 2, "owner", "HUMAN", "create-task-1"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("config version replay err=%v", err)
	}
	wrongConfigTask := task
	wrongConfigTask.ID = "wrong-config-task"
	if err := s.CreateTaskWithConfig(ctx, wrongConfigTask, 2, "owner", "HUMAN", "create-task-wrong-config"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale config task create err=%v", err)
	}
	noopInit := func(context.Context, dbport.Tx) error { return nil }
	makeTask := func(id string) TaskRecord {
		return TaskRecord{ID: id, TenantID: "tenant-a", ProjectID: "project-new", Title: "Source task", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 1}
	}
	if err := s.CreateTaskWith(ctx, makeTask("plain-to-linked"), 1, "owner", "HUMAN", "plain-to-linked-key", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTaskWith(ctx, makeTask("plain-to-linked"), 1, "owner", "HUMAN", "plain-to-linked-key", `{"kind":"DEPLOYED_DOCUMENT","id":"doc-1"}`, noopInit); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("plain to linked retry err=%v", err)
	}
	if err := s.CreateTaskWith(ctx, makeTask("linked-to-plain"), 1, "owner", "HUMAN", "linked-to-plain-key", `{"kind":"DEPLOYED_DOCUMENT","id":"doc-1"}`, noopInit); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTaskWith(ctx, makeTask("linked-to-plain"), 1, "owner", "HUMAN", "linked-to-plain-key", "", nil); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("linked to plain retry err=%v", err)
	}
	if err := s.CreateTaskWith(ctx, makeTask("changed-link"), 1, "owner", "HUMAN", "changed-link-key", `{"kind":"DEPLOYED_DOCUMENT","id":"doc-1"}`, noopInit); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTaskWith(ctx, makeTask("changed-link"), 1, "owner", "HUMAN", "changed-link-key", `{"kind":"DEPLOYED_DOCUMENT","id":"doc-2"}`, noopInit); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed source reference retry err=%v", err)
	}
	if got, err := s.GetTask(ctx, "tenant-a", "project-new", "task-new"); err != nil || got.Title != "First task" || got.Revision != 1 {
		t.Fatalf("get task: %#v err=%v", got, err)
	}
	tasks, err := s.ListTasks(ctx, "tenant-a", "project-new", "", 10)
	if err != nil || len(tasks) != 4 {
		t.Fatalf("list tasks: %#v err=%v", tasks, err)
	}
	edits := []projectdomain.TaskFieldEdit{{FieldID: "priority-note", Type: "TEXT", CanonicalValue: `"urgent"`}}
	moved, err := s.MoveTask(ctx, "tenant-a", "project-new", "task-new", "doing", 1, 1, edits, "owner", "HUMAN", "move-task-1")
	if err != nil || moved.Revision != 2 || moved.StatusID != "doing" {
		t.Fatalf("move task: %#v err=%v", moved, err)
	}
	replayed, err := s.MoveTask(ctx, "tenant-a", "project-new", "task-new", "doing", 1, 1, edits, "owner", "HUMAN", "move-task-1")
	if err != nil || replayed.Revision != 2 {
		t.Fatalf("move replay: %#v err=%v", replayed, err)
	}
	if _, err = s.MoveTask(ctx, "tenant-a", "project-new", "task-new", "done", 1, 1, nil, "owner", "HUMAN", "move-task-stale"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale move err=%v", err)
	}
	if _, err = s.MoveTask(ctx, "tenant-a", "project-new", "task-new", "done", 2, 2, nil, "owner", "HUMAN", "move-config-stale"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale move config err=%v", err)
	}
	var eventCount int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_outbox WHERE tenant_id='tenant-a' AND project_id='project-new'`).Scan(&eventCount)
	}); err != nil {
		t.Fatal(err)
	}
	if eventCount != 6 {
		t.Fatalf("outbox count %d, want six committed mutations", eventCount)
	}
	settings, err := s.UpdateProjectSettings(ctx, "tenant-a", "project-new", "Launch updated", "Europe/Paris", 1, "owner", "HUMAN", "project-settings-1")
	if err != nil || settings.Revision != 2 || settings.Name != "Launch updated" || settings.Timezone != "Europe/Paris" {
		t.Fatalf("project settings update: %#v err=%v", settings, err)
	}
	settingsReplay, err := s.UpdateProjectSettings(ctx, "tenant-a", "project-new", "Launch updated", "Europe/Paris", 1, "owner", "HUMAN", "project-settings-1")
	if err != nil || settingsReplay.Revision != 2 {
		t.Fatalf("project settings replay: %#v err=%v", settingsReplay, err)
	}
	if _, err := s.UpdateProjectSettings(ctx, "tenant-a", "project-new", "Other", "UTC", 1, "owner", "HUMAN", "project-settings-1"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed settings replay err=%v", err)
	}
	if _, err := s.UpdateProjectSettings(ctx, "tenant-b", "project-new", "Cross tenant", "UTC", 2, "owner", "HUMAN", "cross-tenant-settings"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant project settings err=%v", err)
	}
	archivedProject, err := s.TransitionProject(ctx, "tenant-a", "project-new", projectdomain.LifecycleArchived, 2, "owner", "HUMAN", "project-archive-1")
	if err != nil || archivedProject.Lifecycle != string(projectdomain.LifecycleArchived) || archivedProject.Revision != 3 {
		t.Fatalf("project archive: %#v err=%v", archivedProject, err)
	}
	archiveReplay, err := s.TransitionProject(ctx, "tenant-a", "project-new", projectdomain.LifecycleArchived, 2, "owner", "HUMAN", "project-archive-1")
	if err != nil || archiveReplay.Revision != 3 {
		t.Fatalf("project archive replay: %#v err=%v", archiveReplay, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE project SET owner_id='new-owner' WHERE tenant_id='tenant-a' AND id='project-new'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransitionProject(ctx, "tenant-a", "project-new", projectdomain.LifecycleArchived, 2, "owner", "HUMAN", "project-archive-1"); !errors.Is(err, projectdomain.ErrNotAuthorized) {
		t.Fatalf("former owner replay err=%v", err)
	}
	if _, err := s.TransitionProject(ctx, "tenant-a", "project-new", projectdomain.LifecycleActive, 3, "new-owner", "HUMAN", "project-restore-1"); err != nil {
		t.Fatalf("project restore: %v", err)
	}
	if _, err := s.TransitionProject(ctx, "tenant-a", "project-new", projectdomain.LifecycleArchived, 3, "new-owner", "HUMAN", "stale-archive"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale project archive err=%v", err)
	}
	snapshots, err := s.ActiveTaskSnapshots(ctx, "tenant-a", "project-new")
	if err != nil {
		t.Fatal(err)
	}
	var foundMoved bool
	for _, snapshot := range snapshots {
		if snapshot.ID == "task-new" {
			foundMoved = snapshot.StatusID == "doing" && string(snapshot.Fields["priority-note"]) == `"urgent"`
		}
	}
	if !foundMoved {
		t.Fatalf("migration snapshot omitted committed move/custom field: %#v", snapshots)
	}
	role := createProjectRLSRole(t, s, schema)
	err = runProjectAsTenantRole(ctx, s, role, "tenant-a", func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project`).Scan(&count); err != nil {
			return err
		}
		if count != 2 {
			return fmt.Errorf("tenant-a sees %d projects, want only its own", count)
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id) VALUES('tenant-b','forged','tenant-b-project','forged','todo')`)
		return err
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "row-level security") {
		t.Fatalf("tenant-b insert under tenant-a scope err=%v", err)
	}
	err = runProjectAsTenantRole(ctx, s, role, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id) VALUES('tenant-a','task-a','tenant-a-project','Task','todo')`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO project_activity(tenant_id,id,project_id,aggregate_id,actor_id,origin,event_type,new_revision) VALUES('tenant-a','activity-a','tenant-a-project','task-a','owner','HUMAN','task.created',1)`)
		return err
	})
	if err != nil {
		t.Fatalf("tenant-a scoped writes: %v", err)
	}
	for _, statement := range []string{
		`UPDATE project_activity SET event_type='forged' WHERE tenant_id='tenant-a' AND id='activity-a'`,
		`DELETE FROM project_activity WHERE tenant_id='tenant-a' AND id='activity-a'`,
	} {
		err = runProjectAsTenantRole(ctx, s, role, "tenant-a", func(tx dbport.Tx) error { _, err := tx.Exec(ctx, statement); return err })
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "append-only") {
			t.Errorf("activity mutation %q err=%v", statement, err)
		}
	}
}

func TestTodo_PM_003_Security(t *testing.T) {
	s, schema := projectFixture(t)
	ctx := context.Background()
	err := s.RunTx(ctx, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT c.relname,c.relrowsecurity,c.relforcerowsecurity,COALESCE(pg_get_expr(p.polqual,p.polrelid),''),COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),'') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_policy p ON p.polrelid=c.oid WHERE n.nspname=$1 AND c.relkind='r' AND (c.relname='project' OR left(c.relname,8)='project_') ORDER BY c.relname`, schema)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var name, using, check string
			var enabled, forced bool
			if err := rows.Scan(&name, &enabled, &forced, &using, &check); err != nil {
				return err
			}
			count++
			if !enabled || !forced || !strings.Contains(using, "tenant_id") || !strings.Contains(using, "hcmnext.tenant_id") || !strings.Contains(check, "tenant_id") || !strings.Contains(check, "hcmnext.tenant_id") {
				return fmt.Errorf("table %s RLS enabled=%t forced=%t using=%q check=%q", name, enabled, forced, using, check)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count < 7 {
			return fmt.Errorf("found %d project tables, want at least 7", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func seedCurrentWorkflow(t *testing.T, s *Store, tenantID, projectID, actorID string, version int64) {
	t.Helper()
	err := s.RunTenantTx(context.Background(), tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(context.Background(), `INSERT INTO project_workflow_version(tenant_id,project_id,version,source_revision,config_json,digest,publisher_id) VALUES($1,$2,$3,1,'{}'::jsonb,repeat('a',64),$4)`, tenantID, projectID, version, actorID); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `INSERT INTO project_workflow_current(tenant_id,project_id,version) VALUES($1,$2,$3)`, tenantID, projectID, version)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func createProjectRLSRole(t *testing.T, s *Store, schema string) string {
	t.Helper()
	role := "project_rls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	qrole, qschema := quoteIdentifier(role), quoteIdentifier(schema)
	ctx := context.Background()
	if err := s.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE ROLE `+qrole+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT USAGE ON SCHEMA `+qschema+` TO `+qrole); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA `+qschema+` TO `+qrole); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA `+qschema+` TO `+qrole)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.RunTx(context.Background(), func(tx dbport.Tx) error {
			if _, err := tx.Exec(context.Background(), `DROP OWNED BY `+qrole); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DROP ROLE `+qrole)
			return err
		}); err != nil {
			t.Errorf("drop project RLS role: %v", err)
		}
	})
	return role
}

func runProjectAsTenantRole(ctx context.Context, s *Store, role, tenant string, fn func(dbport.Tx) error) error {
	return s.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+quoteIdentifier(role)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant); err != nil {
			return err
		}
		return fn(tx)
	})
}

func quoteIdentifier(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
