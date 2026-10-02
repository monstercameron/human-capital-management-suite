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

// TestTodo_CHATLIVE_003_RuntimeRoleMayRemoveSaved reproduces the review cell's
// roles: the serving role holds only the schema's default table privileges
// (SELECT, INSERT, UPDATE), so saving worked while pressing the bookmark of an
// already saved message, which unsaves it with a DELETE, was refused by the
// database and answered 503. Migration 28 must let the role remove its rows.
func TestTodo_CHATLIVE_003_RuntimeRoleMayRemoveSaved(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	role := "chatlive003_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	db.Exec(t, `CREATE ROLE `+role+` NOLOGIN`)
	t.Cleanup(func() {
		_ = db.ExecErr(`DROP OWNED BY ` + role)
		_ = db.ExecErr(`DROP ROLE ` + role)
	})
	db.Exec(t, `GRANT USAGE ON SCHEMA `+db.Schema+` TO `+role)
	db.Exec(t, `ALTER DEFAULT PRIVILEGES IN SCHEMA `+db.Schema+` GRANT SELECT, INSERT, UPDATE ON TABLES TO `+role)
	migrations, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 27); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO chat_saved_item(tenant_id,host_tenant_id,home_tenant_id,person_id,conversation_id,post_id) VALUES('t','t','t','p','c','post')`)

	remove := func() error {
		conn := db.NewConn(t)
		if _, err := conn.Exec(ctx, `SET ROLE `+role); err != nil {
			t.Fatal(err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id','t',true),set_config('hcmnext.saved_home_tenant_id','t',true),set_config('hcmnext.saved_person_id','p',true)`); err != nil {
			t.Fatal(err)
		}
		removed, err := tx.Exec(ctx, `DELETE FROM chat_saved_item WHERE tenant_id='t' AND home_tenant_id='t' AND person_id='p' AND conversation_id='c' AND post_id='post' AND host_tenant_id='t'`)
		if err != nil {
			return err
		}
		if removed != 1 {
			t.Fatalf("removed %d saved rows, want 1", removed)
		}
		return tx.Commit(ctx)
	}
	var denied *pgconn.PgError
	if err := remove(); !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("before migration 28 the serving role's unsave = %v; want the permission refusal the cell answered 503 for", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := remove(); err != nil {
		t.Fatalf("after migration 28 the serving role cannot unsave: %v", err)
	}
}
