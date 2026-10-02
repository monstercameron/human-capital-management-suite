package chatstore

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// TestTodo_CHATBUG_087_Grant reproduces a cell whose serving role is granted
// SELECT, INSERT and UPDATE on the tables that exist after the migrations of
// 2026-09 ran: the grant loops of migrations 31 and 39 found no role then, so
// removing a glossary term or correcting a message's language was refused by the
// database. Migration 41 gives each role that may update posts what these
// features need, and nothing outside them.
func TestTodo_CHATBUG_087_Grant(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	role := "chatbug087_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	db.Exec(t, `CREATE ROLE `+role+` NOLOGIN`)
	t.Cleanup(func() {
		_ = db.ExecErr(`DROP OWNED BY ` + role)
		_ = db.ExecErr(`DROP ROLE ` + role)
	})
	db.Exec(t, `GRANT USAGE ON SCHEMA `+db.Schema+` TO `+role)
	migrations, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 40); err != nil {
		t.Fatal(err)
	}
	// The role is granted after the tables exist, as a one-off blanket grant.
	db.Exec(t, `GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA `+db.Schema+` TO `+role)
	db.Exec(t, `INSERT INTO chatlang_glossary(tenant_id,term_id,kind,source_term,created_by) VALUES('t','g1','keep','Acme','admin')`)
	db.Exec(t, `INSERT INTO chatlang_setting(tenant_id,conversation_id) VALUES('t','c')`)

	as := func(statement string) error {
		conn := db.NewConn(t)
		if _, err := conn.Exec(ctx, `SET ROLE `+role); err != nil {
			t.Fatal(err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id','t',true)`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, statement); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	deletes := []string{
		`DELETE FROM chatlang_glossary WHERE tenant_id='t' AND term_id='g1'`,
		`DELETE FROM chatlang_setting WHERE tenant_id='t' AND conversation_id='c'`,
		`DELETE FROM chatrender_rendering WHERE tenant_id='t'`,
		`DELETE FROM chatrender_job WHERE tenant_id='t'`,
	}
	var denied *pgconn.PgError
	for _, statement := range deletes {
		if err := as(statement); !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatalf("before migration 41 the serving role's %q = %v; want the permission refusal the page answered with", statement, err)
		}
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, statement := range deletes {
		if err := as(statement); err != nil {
			t.Fatalf("after migration 41 the serving role cannot run %q: %v", statement, err)
		}
	}
	// Nothing beyond the features: the usage lines stay append-only and the posts
	// themselves are not deletable.
	for _, statement := range []string{`DELETE FROM chatlang_usage WHERE tenant_id='t'`, `DELETE FROM chat_post WHERE tenant_id='t'`} {
		if err := as(statement); !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatalf("the serving role can run %q: %v", statement, err)
		}
	}
	// The preference write the page makes works with those privileges.
	if err := as(`INSERT INTO chat_preference(tenant_id,home_tenant_id,member_id,conversation_id,marker,value) VALUES('t','t','m','','chatrender.language.v1','{}') ON CONFLICT(tenant_id,home_tenant_id,member_id,conversation_id,marker) DO UPDATE SET value=EXCLUDED.value,revision=chat_preference.revision+1,updated_at=now()`); err != nil {
		t.Fatalf("the reading language write: %v", err)
	}
}
