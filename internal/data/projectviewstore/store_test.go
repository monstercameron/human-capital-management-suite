package projectviewstore

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func fixture(t *testing.T) (*projectstore.Store, *Store, string) {
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
	projects, err := projectstore.New(context.Background(), projectstore.Config{DSN: db.URL, CoreDSN: "postgres://other:secret@127.0.0.1:5432/postgres?sslmode=disable", Schema: db.Schema, MaxConns: 4, MinConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(projects.Close)
	if err := projects.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES('tenant-a','project-a','owner','A','UTC')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return projects, New(projects), db.Schema
}

func sampleView(id string, audience projectboard.Audience) projectboard.BoardView {
	return projectboard.BoardView{ID: id, Name: "Saved " + id, Version: 1, Audience: audience, Columns: []projectboard.Column{{ID: "todo", Label: "To do", StatusIDs: []string{"todo"}}}, Grouping: projectboard.Grouping{Kind: projectboard.GroupNone}, CardFields: []string{projectboard.CardTitle}, SwimlaneValueOrder: []string{"preferred", "other"}}
}

func TestTodo_PM_016_Integration(t *testing.T) {
	projects, store, _ := fixture(t)
	ctx := context.Background()
	if err := projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES('tenant-a','project-b','owner','B','UTC')`); err != nil {
			return err
		}
		return InitializeProjectTx(ctx, tx, "tenant-a", "project-b", "owner")
	}); err != nil {
		t.Fatalf("initialize default view: %v", err)
	}
	defaultView, err := store.GetView(ctx, "tenant-a", "project-b", "alice", "default")
	if err != nil || defaultView.Version != 1 || len(defaultView.Columns) != 3 || defaultView.Name != "Task board" || defaultView.SwimlaneValueOrder == nil {
		t.Fatalf("default view: %#v err=%v", defaultView, err)
	}
	personal := sampleView("mine", projectboard.AudiencePersonal)
	got, err := store.SaveView(ctx, "tenant-a", "project-a", "alice", "alice", personal, 0, "save-mine-1")
	if err != nil || got.Version != 1 || got.Name != personal.Name || len(got.SwimlaneValueOrder) != 2 || got.SwimlaneValueOrder[0] != "preferred" {
		t.Fatalf("create personal view: %#v err=%v", got, err)
	}
	if replay, err := store.SaveView(ctx, "tenant-a", "project-a", "alice", "alice", personal, 0, "save-mine-1"); err != nil || replay.Version != 1 {
		t.Fatalf("exact retry: %#v err=%v", replay, err)
	}
	changedReplay := personal
	changedReplay.Columns[0].Label = "Other"
	if _, err := store.SaveView(ctx, "tenant-a", "project-a", "alice", "alice", changedReplay, 0, "save-mine-1"); !errors.Is(err, projectstore.ErrIdempotencyConflict) {
		t.Fatalf("changed idempotency replay err=%v", err)
	}
	project := sampleView("team", projectboard.AudienceProject)
	if _, err := store.SaveView(ctx, "tenant-a", "project-a", "", "owner", project, 0, "save-team-1"); err != nil {
		t.Fatalf("create project view: %v", err)
	}
	if _, err := store.GetView(ctx, "tenant-a", "project-a", "bob", "mine"); !errors.Is(err, projectstore.ErrNotFound) {
		t.Fatalf("other user's personal view err=%v", err)
	}
	if got, err := store.GetView(ctx, "tenant-a", "project-a", "bob", "team"); err != nil || got.Audience != projectboard.AudienceProject || got.Name != project.Name || len(got.SwimlaneValueOrder) != 2 || got.SwimlaneValueOrder[1] != "other" {
		t.Fatalf("read project view: %#v err=%v", got, err)
	}
	personal.Columns[0].Label = "Doing"
	personal.Name = "My doing board"
	personal.SwimlaneValueOrder = []string{"other", "preferred"}
	got, err = store.SaveView(ctx, "tenant-a", "project-a", "alice", "alice", personal, 1, "save-mine-2")
	if err != nil || got.Version != 2 || got.Name != personal.Name || got.SwimlaneValueOrder[0] != "other" {
		t.Fatalf("update view: %#v err=%v", got, err)
	}
	if _, err := store.SaveView(ctx, "tenant-a", "project-a", "alice", "alice", personal, 1, "save-mine-stale"); !errors.Is(err, projectstore.ErrRevisionConflict) {
		t.Fatalf("stale revision err=%v", err)
	}
	views, err := store.ListViews(ctx, "tenant-a", "project-a", "bob", "", 10)
	if err != nil || len(views) != 1 || views[0].ID != "team" || views[0].Name != project.Name || views[0].SwimlaneValueOrder[0] != "preferred" {
		t.Fatalf("bounded visible list: %#v err=%v", views, err)
	}
	for i := 0; i < 18; i++ {
		view := sampleView("view-"+string(rune('a'+i)), projectboard.AudienceProject)
		if _, err := store.SaveView(ctx, "tenant-a", "project-a", "", "owner", view, 0, "save-"+view.ID); err != nil {
			t.Fatalf("create view %s: %v", view.ID, err)
		}
	}
	if _, err := store.SaveView(ctx, "tenant-a", "project-a", "", "owner", sampleView("view-over-limit", projectboard.AudienceProject), 0, "save-over-limit"); !errors.Is(err, ErrViewLimit) {
		t.Fatalf("project view cap err=%v", err)
	}
}

func TestTodo_PM_016_Security(t *testing.T) {
	projects, store, schema := fixture(t)
	role := "project_view_rls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	qrole, qschema := `"`+role+`"`, `"`+schema+`"`
	ctx := context.Background()
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
		if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
			if _, err := tx.Exec(ctx, `DROP OWNED BY `+qrole); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `DROP ROLE `+qrole)
			return err
		}); err != nil {
			t.Errorf("drop role: %v", err)
		}
	})
	err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+qrole); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id','tenant-a',true)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_board_view(tenant_id,project_id,id,audience,owner_id,revision,config_json) VALUES('tenant-b','project-b','bad','PROJECT','',1,'{}')`)
		return err
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "row-level security") {
		t.Fatalf("cross-tenant view write err=%v", err)
	}
	if _, err := store.SaveView(ctx, "tenant-a", "project-a", "", "owner", sampleView("bad-owner", projectboard.AudiencePersonal), 0, "bad-owner"); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("personal view without owner err=%v", err)
	}
}
