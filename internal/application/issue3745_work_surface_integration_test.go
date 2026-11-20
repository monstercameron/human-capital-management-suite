package application

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectmemberstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectviewstore"
)

func issue3745MigrateUp(t *testing.T, db *pgtest.DB, migrations fs.FS) {
	t.Helper()
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, sub, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestIssue3745_RealStoresServeOpenAndWithholdRestricted_Integration runs the
// preparation against the real document and project stores (embedded
// PostgreSQL), not fakes: the reviewer and every colleague can read the open
// document and project, only the administrator and the reviewer can read the
// restricted ones, an outsider reads neither, and a rerun changes nothing.
func TestIssue3745_RealStoresServeOpenAndWithholdRestricted_Integration(t *testing.T) {
	ctx := context.Background()
	docDB, projDB := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
	issue3745MigrateUp(t, docDB, documenthubstore.Migrations)
	issue3745MigrateUp(t, projDB, projectstore.Migrations)
	documents, err := documenthubstore.New(ctx, documenthubstore.Config{DSN: personaChatSchemaDSN(t, docDB.URL, docDB.Schema), ChatDSN: "postgres://chat@127.0.0.1:1/chat", CoreDSN: "postgres://core@127.0.0.1:1/core"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(documents.Close)
	store, err := projectstore.New(ctx, projectstore.Config{DSN: projDB.URL, CoreDSN: "postgres://other:secret@127.0.0.1:5432/postgres?sslmode=disable", Schema: projDB.Schema, MaxConns: 4, MinConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	members, err := projectmemberstore.New(store)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &projectservice.StoreAdapter{Projects: store, Members: members, Configs: projectconfigstore.New(store), Views: projectviewstore.New(store)}

	people := issue3745People(5)
	deps := WorkSurfaceDeps{Projects: adapter, Documents: documents, Tenant: harborcareDemoTenant, Admin: harborcareDemoAdmin, People: people}
	now := time.Now().UTC()
	receipt, err := ensureWorkSurface(ctx, deps, now)
	if err != nil {
		t.Fatalf("prepare against real stores: %v", err)
	}
	if !receipt.Changed() || len(receipt.Items) != 6 {
		t.Fatalf("receipt = %+v", receipt)
	}
	again, err := ensureWorkSurface(ctx, deps, now)
	if err != nil || again.Changed() {
		t.Fatalf("rerun = %+v, %v, want nothing added", again, err)
	}

	outsider := "someone-outside"
	titlesFor := func(user string) map[string]bool {
		list, err := documents.ListPersonalDocuments(ctx, harborcareDemoTenant, user, 50)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, d := range list {
			out[d.Title] = true
		}
		return out
	}
	projectsFor := func(user string) map[string]bool {
		list, err := adapter.ListAuthorizedProjects(ctx, harborcareDemoTenant, user, "", 50)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, p := range list {
			out[p.ID] = true
		}
		return out
	}
	cases := []struct {
		who                              string
		openDoc, restrictedDoc, openProj bool
		restrictedProj                   bool
	}{
		{harborcareDemoAdmin, true, true, true, true},
		{receipt.Reviewer.Key, true, true, true, true},
		{receipt.Colleagues[0].Key, true, false, true, false},
		{outsider, false, false, false, false},
	}
	for _, c := range cases {
		docs, projs := titlesFor(c.who), projectsFor(c.who)
		if docs[workSurfaceOpenDocumentTitle] != c.openDoc || docs[workSurfaceRestrictedDocTitle] != c.restrictedDoc || projs[WorkSurfaceOpenProjectID] != c.openProj || projs[WorkSurfaceRestrictedProjectID] != c.restrictedProj {
			t.Errorf("%s sees docs=%v projects=%v, want openDoc=%v restrictedDoc=%v openProject=%v restrictedProject=%v", c.who, docs, projs, c.openDoc, c.restrictedDoc, c.openProj, c.restrictedProj)
		}
	}
	// Another tenant sees none of it.
	if docs, err := documents.ListPersonalDocuments(ctx, "ironridge-demo", harborcareDemoAdmin, 50); err != nil || len(docs) != 0 {
		t.Fatalf("other tenant documents = %v, %v", docs, err)
	}
}
