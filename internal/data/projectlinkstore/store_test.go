package projectlinkstore

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
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func linkFixture(t *testing.T) (*Store, *projectstore.Store, string) {
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
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	projects, err := projectstore.New(context.Background(), projectstore.Config{DSN: db.URL, Schema: db.Schema, MaxConns: 4, MinConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(projects.Close)
	links, err := New(projects)
	if err != nil {
		t.Fatal(err)
	}
	return links, projects, db.Schema
}

func TestTodo_PM_020(t *testing.T) {
	links, projects, schema := linkFixture(t)
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		project := projectstore.ProjectRecord{ID: tenant + "-project", TenantID: tenant, OwnerID: "owner", Name: "Project", Timezone: "UTC"}
		if err := projects.CreateProject(ctx, project, "owner", "HUMAN", tenant+"-create-project"); err != nil {
			t.Fatal(err)
		}
		task := projectstore.TaskRecord{ID: tenant + "-task", TenantID: tenant, ProjectID: project.ID, Title: "Task", StatusID: "todo"}
		if err := projects.CreateTask(ctx, task, "owner", "HUMAN", tenant+"-create-task"); err != nil {
			t.Fatal(err)
		}
	}
	cases := []projectlink.Reference{
		{Kind: projectlink.ChatConversation, ID: "conversation-1"},
		{Kind: projectlink.ChatPost, ID: "post-1", ConversationID: "conversation-1"},
		{Kind: projectlink.DeployedDocument, ID: "document-1", Version: "version-7", ScopeID: "team-1"},
		{Kind: projectlink.WorkItem, ID: "work-item-1"},
	}
	for i, ref := range cases {
		record := LinkRecord{ID: fmt.Sprintf("link-%d", i), TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: ref}
		if err := links.Add(ctx, record); err != nil {
			t.Fatalf("add %s: %v", ref.Kind, err)
		}
		if err := links.Add(ctx, record); err != nil {
			t.Fatalf("idempotent add %s: %v", ref.Kind, err)
		}
	}
	changed := LinkRecord{ID: "link-0", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: projectlink.Reference{Kind: projectlink.ChatConversation, ID: "other-conversation"}}
	if err := links.Add(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("different replay err=%v", err)
	}
	page, err := links.List(ctx, "tenant-a", "tenant-a-project", "tenant-a-task", "", 2)
	if err != nil || len(page) != 2 || page[0].Reference.ID != "conversation-1" || page[1].Reference.ID != "post-1" {
		t.Fatalf("first page=%#v err=%v", page, err)
	}
	page2, err := links.List(ctx, "tenant-a", "tenant-a-project", "tenant-a-task", page[1].ID, 2)
	if err != nil || len(page2) != 2 || page2[0].Reference.Version != "version-7" || page2[0].Reference.ScopeID != "team-1" || page2[1].Reference.Kind != projectlink.WorkItem {
		t.Fatalf("second page=%#v err=%v", page2, err)
	}
	other, err := links.List(ctx, "tenant-b", "tenant-a-project", "tenant-a-task", "", 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-tenant list=%#v err=%v", other, err)
	}
	if err := links.Add(ctx, LinkRecord{ID: "forged", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-b-task", Reference: cases[0]}); err == nil {
		t.Fatal("cross-project task link accepted")
	}
	if err := links.Add(ctx, LinkRecord{ID: "bad-doc", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: projectlink.Reference{Kind: projectlink.DeployedDocument, ID: "document-1", Version: "version-7"}}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("missing doc scope err=%v", err)
	}
	if _, err := links.List(ctx, "tenant-a", "tenant-a-project", "tenant-a-task", "", 101); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("unbounded list err=%v", err)
	}
	var rls, forced bool
	// Verify RLS is enabled and forced on the typed link table.
	if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT relrowsecurity,relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname='project_task_link'`, schema).Scan(&rls, &forced)
	}); err != nil {
		t.Fatal(err)
	}
	if !rls || !forced {
		t.Fatalf("link RLS enabled=%v forced=%v", rls, forced)
	}
	role := "project_link_rls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	qrole, qschema := `"`+role+`"`, `"`+schema+`"`
	if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE ROLE `+qrole+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT USAGE ON SCHEMA `+qschema+` TO `+qrole); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA `+qschema+` TO `+qrole)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := projects.RunTx(context.Background(), func(tx dbport.Tx) error {
			if _, err := tx.Exec(context.Background(), `DROP OWNED BY `+qrole); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DROP ROLE `+qrole)
			return err
		}); err != nil {
			t.Errorf("drop RLS role: %v", err)
		}
	})
	if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+qrole); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id','tenant-b',true)`); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_task_link`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("tenant-b sees %d tenant-a links", count)
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_task_link(tenant_id,id,project_id,task_id,kind,target_id) VALUES('tenant-a','rls-forged','tenant-a-project','tenant-a-task','WORK_ITEM','work-item-x')`)
		return err
	}); err == nil {
		t.Fatal("cross-tenant link insert passed RLS")
	}

	rollback := errors.New("rollback task and link together")
	err = projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id) VALUES('tenant-a','atomic-task','tenant-a-project','Atomic','todo')`); err != nil {
			return err
		}
		link := LinkRecord{ID: "atomic-link", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "atomic-task", Reference: cases[0]}
		if err := AddTx(ctx, tx, link); err != nil {
			return err
		}
		if err := AddTx(ctx, tx, link); err != nil {
			return fmt.Errorf("same-transaction replay: %w", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("atomic task/link transaction err=%v", err)
	}
	if _, err := projects.GetTask(ctx, "tenant-a", "tenant-a-project", "atomic-task"); !errors.Is(err, projectstore.ErrNotFound) {
		t.Fatalf("rolled-back task lookup err=%v", err)
	}
	if page, err := links.List(ctx, "tenant-a", "tenant-a-project", "atomic-task", "", 10); err != nil || len(page) != 0 {
		t.Fatalf("rolled-back link page=%#v err=%v", page, err)
	}
	err = projects.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		return AddTx(ctx, tx, LinkRecord{ID: "wrong-tenant", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: cases[0]})
	})
	if !errors.Is(err, ErrTenantScope) {
		t.Fatalf("transaction tenant mismatch err=%v", err)
	}

	newLink := LinkRecord{ID: "revisioned-link", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: projectlink.Reference{Kind: projectlink.ChatConversation, ID: "revisioned-conversation"}}
	addedRevision, err := links.AddRevisioned(ctx, newLink, "owner", "add-link-key", 1)
	if err != nil || addedRevision != 2 {
		t.Fatalf("revisioned add revision=%d err=%v", addedRevision, err)
	}
	replayedRevision, err := links.AddRevisioned(ctx, newLink, "owner", "add-link-key", 1)
	if err != nil || replayedRevision != 2 {
		t.Fatalf("add replay revision=%d err=%v", replayedRevision, err)
	}
	changedAdd := newLink
	changedAdd.Reference.ID = "different-conversation"
	if _, err := links.AddRevisioned(ctx, changedAdd, "owner", "add-link-key", 1); !errors.Is(err, projectstore.ErrIdempotencyConflict) {
		t.Fatalf("changed add retry err=%v", err)
	}
	if _, err := links.AddRevisioned(ctx, newLink, "owner", "different-key", 1); !errors.Is(err, projectstore.ErrRevisionConflict) {
		t.Fatalf("stale add err=%v", err)
	}
	if _, err := links.Remove(ctx, "tenant-b", "tenant-a-project", "tenant-a-task", newLink.ID, "owner", "cross-tenant-remove", 2); !errors.Is(err, projectstore.ErrNotFound) {
		t.Fatalf("cross-tenant remove err=%v", err)
	}
	removedRevision, err := links.Remove(ctx, "tenant-a", "tenant-a-project", "tenant-a-task", newLink.ID, "owner", "remove-link-key", 2)
	if err != nil || removedRevision != 3 {
		t.Fatalf("remove revision=%d err=%v", removedRevision, err)
	}
	removedRetry, err := links.Remove(ctx, "tenant-a", "tenant-a-project", "tenant-a-task", newLink.ID, "owner", "remove-link-key", 2)
	if err != nil || removedRetry != 3 {
		t.Fatalf("remove retry revision=%d err=%v", removedRetry, err)
	}
	if _, err := links.Remove(ctx, "tenant-a", "tenant-a-project", "tenant-a-task", "another-link", "owner", "remove-link-key", 2); !errors.Is(err, projectstore.ErrIdempotencyConflict) {
		t.Fatalf("changed remove retry err=%v", err)
	}
	visible, err := links.List(ctx, "tenant-a", "tenant-a-project", "tenant-a-task", "", 20)
	for _, v := range visible {
		if v.ID == newLink.ID {
			t.Fatalf("tombstoned link remained visible: %+v", v)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	var removedBy string
	var removedTaskRevision int64
	if err := projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT removed_by,removed_task_revision FROM project_task_link WHERE tenant_id=$1 AND id=$2`, "tenant-a", newLink.ID).Scan(&removedBy, &removedTaskRevision)
	}); err != nil {
		t.Fatal(err)
	}
	if removedBy != "owner" || removedTaskRevision != 3 {
		t.Fatalf("tombstone actor=%q revision=%d", removedBy, removedTaskRevision)
	}
	var activityCount, outboxCount int
	var eventPayload []byte
	if err := projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type IN ('task.link_added','task.link_removed')`, "tenant-a", "tenant-a-project", "tenant-a-task").Scan(&activityCount); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_outbox WHERE tenant_id=$1 AND project_id=$2 AND event_type IN ('task.link_added','task.link_removed')`, "tenant-a", "tenant-a-project").Scan(&outboxCount); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT payload FROM project_outbox WHERE tenant_id=$1 AND project_id=$2 AND event_type='task.link_removed'`, "tenant-a", "tenant-a-project").Scan(&eventPayload)
	}); err != nil {
		t.Fatal(err)
	}
	if activityCount != 2 || outboxCount != 2 {
		t.Fatalf("link activity=%d outbox=%d, want 2 each", activityCount, outboxCount)
	}
	if strings.Contains(string(eventPayload), "revisioned-conversation") {
		t.Fatalf("event leaked target identifier: %s", eventPayload)
	}
	if err := projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE project_task_link SET target_id='rewritten' WHERE tenant_id=$1 AND id=$2`, "tenant-a", newLink.ID)
		return err
	}); err == nil {
		t.Fatal("tombstone target mutation allowed")
	}
}
