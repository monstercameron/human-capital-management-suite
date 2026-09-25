package projectsearch

import (
	"context"
	"io/fs"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_PM_018_StoreAdapterAppliesExactPredicatesInCanonicalStore(t *testing.T) {
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
	store, err := projectstore.New(context.Background(), projectstore.Config{DSN: db.URL, CoreDSN: "postgres://other:secret@127.0.0.1:5432/postgres?sslmode=disable", Schema: db.Schema, MaxConns: 4, MinConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	fields := `{"team":{"FieldID":"team","Type":"ENUM","CanonicalValue":"\"design\""}}`
	if err := store.RunTenantTx(context.Background(), "tenant-search", func(tx dbport.Tx) error {
		if _, err := tx.Exec(context.Background(), `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES('tenant-search','p1','owner','Search','UTC')`); err != nil {
			return err
		}
		for _, row := range []struct{ id, status, assignee, typ, due, field string }{
			{"01", "todo", "alice", "bug", "2026-02-28", fields},
			{"02", "todo", "alice", "bug", "2026-03-01", fields},
			{"03", "doing", "alice", "bug", "2026-03-02", fields},
			{"04", "todo", "bob", "bug", "2026-03-02", fields},
			{"05", "todo", "alice", "feature", "2026-03-02", fields},
		} {
			if _, err := tx.Exec(context.Background(), `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,assignee_id,type_id,due_date,fields_json) VALUES('tenant-search',$1,'p1','Task',$2,$3,$4,$5,$6::jsonb)`, row.id, row.status, row.assignee, row.typ, row.due, row.field); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	repo := StoreRepository{Store: store}
	filter := Filter{StatusIDs: []string{"todo"}, AssigneeID: "alice", TypeIDs: []string{"bug"}, DueDateFrom: "2026-03-01", DueDateTo: "2026-03-03", Fields: map[string][]string{"team": {"design"}}}
	first, err := repo.ListExact(context.Background(), "tenant-search", "p1", "", filter, 2)
	if err != nil || len(first) != 1 || first[0].ID != "02" || first[0].Fields["team"].Type != "ENUM" || first[0].Fields["team"].CanonicalValue != `"design"` || first[0].DueDate != "2026-03-01" {
		t.Fatalf("canonical exact query=%+v err=%v", first, err)
	}
	if _, err := (StoreRepository{}).ListExact(context.Background(), "tenant-search", "p1", "", Filter{}, 1); err != ErrUnavailable {
		t.Fatalf("nil store error=%v", err)
	}
}
